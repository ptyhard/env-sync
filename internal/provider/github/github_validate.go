package github

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// githubValidateOsExit はテストで差し替え可能な終了関数。
var githubValidateOsExit = os.Exit

// githubStdoutWriter はテストで差し替え可能な標準出力先。
// Validate の全出力はこのライタへ書く。
var githubStdoutWriter io.Writer = os.Stdout

// Validate は GitHub ターゲットの認証・到達確認を読み取り専用で行う。
// GET /repos/{owner}/{repo} のみを使用し、環境変数の登録・変更は行わない。
func (g *githubProvider) Validate(opts provider.Options, entries []provider.Entry) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}

	targets, err := appCfg.ResolveGitHubTargets(opts.GitHubRepo)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: httpTimeout}
	okCount, ngCount := 0, 0

	for _, tgt := range targets {
		// owner/repo を解決
		ownerStr, repoStr, resolveErr := resolveOwnerRepo(tgt, appCfg)

		// リポジトリの取得元を決定（git remote フォールバックが使われた場合）
		repoSrc := tgt.RepoSource
		if tgt.Repo == "" && resolveErr == nil {
			repoSrc = "git_remote"
		}

		targetLabel := tgt.Name
		if targetLabel == "" {
			if resolveErr == nil {
				targetLabel = ownerStr + "/" + repoStr
			} else {
				targetLabel = i18n.T(i18n.MsgValidateSourceUnset)
			}
		}
		fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateHeader, targetLabel))

		// token 表示（値は出さずマスク）
		if tgt.Token == "" {
			fmt.Fprintf(githubStdoutWriter, "  token     : %s\n", i18n.T(i18n.MsgValidateTokenUnset))
		} else {
			fmt.Fprintf(githubStdoutWriter, "  token     : %s\n", i18n.T(i18n.MsgValidateTokenMasked, githubSourceLabel(tgt.TokenSource)))
		}

		// repo 表示（解決失敗時も MsgValidateGitHubRepo テンプレートで統一し二重カッコを回避）
		if resolveErr != nil {
			fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateGitHubRepo, i18n.T(i18n.MsgValidateSourceUnset), githubSourceLabel(repoSrc)))
			fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateRepoUnresolvableSkip))
			ngCount++
			continue
		}
		fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateGitHubRepo, ownerStr+"/"+repoStr, githubSourceLabel(repoSrc)))

		// token が未設定なら API 確認をスキップ
		if tgt.Token == "" {
			fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateTokenUnsetSkip))
			ngCount++
			continue
		}

		status, checkErr := githubCheckAccess(client, tgt.Token, ownerStr, repoStr)
		if checkErr != nil {
			fmt.Fprintf(githubStdoutWriter, "  API check : error: %s\n", checkErr)
			ngCount++
			continue
		}

		if status >= 200 && status < 300 {
			fmt.Fprintf(githubStdoutWriter, "  API check : %s %s\n", i18n.T(i18n.MsgValidateHTTPStatus, status), i18n.T(i18n.MsgValidateOK))
			okCount++
		} else {
			fmt.Fprintf(githubStdoutWriter, "  API check : %s\n", i18n.T(i18n.MsgValidateHTTPStatus, status))
			switch status {
			case 404:
				fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateGitHubCause404))
			case 401:
				fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateGitHubCause401))
			case 403:
				fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateGitHubCause403))
			}
			ngCount++
		}
	}

	fmt.Fprint(githubStdoutWriter, i18n.T(i18n.MsgValidateResult, okCount, ngCount))
	if ngCount > 0 {
		githubValidateOsExit(1)
	}
	return nil
}

// githubCheckAccess は GET /repos/{owner}/{repo} で GitHub API への到達確認を行う。
// 成功・失敗に関わらず (statusCode, nil) を返す。HTTP 以外のエラーは err に返す。
func githubCheckAccess(client *http.Client, token, owner, repo string) (statusCode int, err error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s",
		githubAPIBase, url.PathEscape(owner), url.PathEscape(repo))

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return 0, err
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
	return res.StatusCode, nil
}

// githubSourceLabel は取得元識別子をユーザー表示ラベルに変換する。
func githubSourceLabel(src string) string {
	switch src {
	case "env":
		return i18n.T(i18n.MsgValidateSourceEnv)
	case "config":
		return i18n.T(i18n.MsgValidateSourceConfig)
	case "git_remote":
		return i18n.T(i18n.MsgValidateSourceGitRemote)
	default:
		return i18n.T(i18n.MsgValidateSourceUnset)
	}
}
