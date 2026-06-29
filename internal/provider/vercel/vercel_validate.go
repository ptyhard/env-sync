// vercel_validate.go は Vercel provider の validate サブコマンド実装を提供する。
// 読み取り専用（GET のみ）で認証・到達確認を行い、書き込みは行わない。
package vercel

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// validateOsExit はテストで差し替え可能な終了関数。
var validateOsExit = os.Exit

// stdoutWriter はテストで差し替え可能な標準出力先。
// Validate の全出力はこのライタへ書く。
var stdoutWriter io.Writer = os.Stdout

// Validate は Vercel ターゲットの認証・到達確認を読み取り専用で行う。
// GET /v10/projects/{id}/env のみを使用し、環境変数の登録・変更は行わない。
func (v *vercelProvider) Validate(opts provider.Options, _ []provider.Entry) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}

	targets, err := appCfg.ResolveVercelTargets(opts.VercelProject)
	if err != nil {
		return err
	}

	// 単一ターゲット時の .vercel/project.json フォールバック
	if _, err := applyProjectJSONFallback(targets); err != nil {
		return err
	}

	client := &http.Client{Timeout: httpTimeout}
	okCount, ngCount := 0, 0

	for _, tgt := range targets {
		// name と projectId が両方未設定の場合は "(未設定)" をフォールバックラベルとして使う
		targetLabel := tgt.ProjectID
		if tgt.Name != "" {
			targetLabel = tgt.Name
		}
		if targetLabel == "" {
			targetLabel = i18n.T(i18n.MsgValidateSourceUnset)
		}
		fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateHeader, targetLabel))

		// token 表示（値は出さずマスク）
		if tgt.Token == "" {
			fmt.Fprintf(stdoutWriter, "  token     : %s\n", i18n.T(i18n.MsgValidateTokenUnset))
		} else {
			fmt.Fprintf(stdoutWriter, "  token     : %s\n", i18n.T(i18n.MsgValidateTokenMasked, vercelSourceLabel(tgt.TokenSource)))
		}

		// projectId 表示（設定・未設定いずれも MsgValidateVercelProjectID テンプレートで統一）
		pid := tgt.ProjectID
		if pid == "" {
			pid = i18n.T(i18n.MsgValidateSourceUnset)
		}
		fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateVercelProjectID, pid, vercelSourceLabel(tgt.ProjectIDSource)))

		// teamId 表示
		if tgt.TeamID == "" {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateVercelTeamID, i18n.T(i18n.MsgValidateSourceUnset), vercelSourceLabel(tgt.TeamIDSource)))
		} else {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateVercelTeamID, tgt.TeamID, vercelSourceLabel(tgt.TeamIDSource)))
		}

		// token / projectId が未設定なら API 確認をスキップ（それぞれ個別にメッセージを出す）
		if tgt.Token == "" {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateTokenUnsetSkip))
			ngCount++
			continue
		}
		if tgt.ProjectID == "" {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateProjectIDUnsetSkip))
			ngCount++
			continue
		}

		status, _, checkErr := vercelCheckAccess(client, tgt.Token, tgt.ProjectID, tgt.TeamID)
		if checkErr != nil {
			fmt.Fprintf(stdoutWriter, "  API check : error: %s\n", checkErr)
			ngCount++
			continue
		}

		if status >= 200 && status < 300 {
			fmt.Fprintf(stdoutWriter, "  API check : %s %s\n",
				i18n.T(i18n.MsgValidateHTTPStatus, status),
				i18n.T(i18n.MsgValidateOK))
			okCount++
		} else {
			fmt.Fprintf(stdoutWriter, "  API check : %s\n", i18n.T(i18n.MsgValidateHTTPStatus, status))
			switch status {
			case 404:
				fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateVercelCause404))
			case 401:
				fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateVercelCause401))
			case 403:
				fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateVercelCause403))
			}
			ngCount++
		}
	}

	fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateResult, okCount, ngCount))
	if ngCount > 0 {
		validateOsExit(1)
	}
	return nil
}

// vercelSourceLabel は取得元識別子をユーザー表示ラベルに変換する。
func vercelSourceLabel(src string) string {
	switch src {
	case "env":
		return i18n.T(i18n.MsgValidateSourceEnv)
	case "config":
		return i18n.T(i18n.MsgValidateSourceConfig)
	case "project_json":
		return i18n.T(i18n.MsgValidateSourceProjectJSON)
	case "git_remote":
		return i18n.T(i18n.MsgValidateSourceGitRemote)
	default:
		return i18n.T(i18n.MsgValidateSourceUnset)
	}
}
