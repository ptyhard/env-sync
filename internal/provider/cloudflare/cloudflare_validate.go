// cloudflare_validate.go は Cloudflare provider の validate サブコマンド実装を提供する。
// 読み取り専用（GET のみ）で認証・到達確認を行い、書き込みは行わない。
package cloudflare

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

// Validate は Cloudflare ターゲットの認証・到達確認を読み取り専用で行う。
// GET /accounts/{id}/workers/scripts/{name}/secrets のみを使用し、シークレットの登録・変更は行わない。
func (c *cloudflareProvider) Validate(opts provider.Options, _ []provider.Entry) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}

	targets, err := appCfg.ResolveCloudflareTargets(opts.CloudflareScript)
	if err != nil {
		return err
	}

	// 単一ターゲット時の wrangler 設定ファイルフォールバック
	if err := applyWranglerFallback(targets); err != nil {
		return err
	}

	client := &http.Client{Timeout: httpTimeout}
	okCount, ngCount := 0, 0

	for _, tgt := range targets {
		// name と script が両方未設定の場合は "(未設定)" をフォールバックラベルとして使う
		targetLabel := tgt.Script
		if tgt.Name != "" {
			targetLabel = tgt.Name
		}
		if targetLabel == "" {
			targetLabel = i18n.T(i18n.MsgValidateSourceUnset)
		}
		fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateHeader, targetLabel))

		// token 表示（値は出さずマスク）
		if tgt.APIToken == "" {
			fmt.Fprintf(stdoutWriter, "  token     : %s\n", i18n.T(i18n.MsgValidateTokenUnset))
		} else {
			fmt.Fprintf(stdoutWriter, "  token     : %s\n", i18n.T(i18n.MsgValidateTokenMasked, sourceLabel(tgt.TokenSource)))
		}

		// accountId 表示
		accountID := tgt.AccountID
		if accountID == "" {
			accountID = i18n.T(i18n.MsgValidateSourceUnset)
		}
		fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateCloudflareAccountID, accountID, sourceLabel(tgt.AccountIDSource)))

		// script 表示
		script := tgt.Script
		if script == "" {
			script = i18n.T(i18n.MsgValidateSourceUnset)
		}
		fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateCloudflareScript, script, sourceLabel(tgt.ScriptSource)))

		// 未設定項目があれば API 確認をスキップ（それぞれ個別にメッセージを出す）
		if tgt.APIToken == "" {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateTokenUnsetSkip))
			ngCount++
			continue
		}
		if tgt.AccountID == "" {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateAccountIDUnsetSkip))
			ngCount++
			continue
		}
		if tgt.Script == "" {
			fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateScriptUnsetSkip))
			ngCount++
			continue
		}

		status, checkErr := checkAccess(client, tgt.APIToken, tgt.AccountID, tgt.Script)
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
				fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateCloudflareCause404))
			case 401:
				fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateCloudflareCause401))
			case 403:
				fmt.Fprint(stdoutWriter, i18n.T(i18n.MsgValidateCloudflareCause403))
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

// checkAccess は GET .../secrets で Cloudflare API への到達確認を行う。
// HTTP 以外のエラーは err に返す。
func checkAccess(client *http.Client, token, accountID, script string) (statusCode int, err error) {
	req, err := http.NewRequest(http.MethodGet, secretsURL(accountID, script), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
	return res.StatusCode, nil
}

// sourceLabel は取得元識別子をユーザー表示ラベルに変換する。
func sourceLabel(src string) string {
	switch src {
	case "env":
		return i18n.T(i18n.MsgValidateSourceEnv)
	case "config":
		return i18n.T(i18n.MsgValidateSourceConfig)
	case "wrangler":
		return i18n.T(i18n.MsgValidateSourceWrangler)
	default:
		return i18n.T(i18n.MsgValidateSourceUnset)
	}
}
