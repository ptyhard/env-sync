// Package github は GitHub Actions Secrets/Variables への環境変数同期を実装する provider。
package github

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// githubAPIBase は GitHub REST API のベース URL。テストで httptest.Server を
// 指す差し替えができるよう var にしている。
var githubAPIBase = "https://api.github.com"

// httpTimeout は GitHub API 呼び出しの HTTP タイムアウト。
const httpTimeout = 30 * time.Second

func init() {
	provider.RegisterProvider("github", func() provider.Provider { return &githubProvider{} })
}

// githubProvider は GitHub Actions への同期を担当する Provider 実装。
type githubProvider struct{}

func (g *githubProvider) Name() string { return "github" }

// githubTask は GitHub Actions に登録する 1 件のタスク情報を表す。
// envScope が空のときはリポジトリレベル、それ以外は named environment スコープ。
type githubTask struct {
	envScope string
	entry    provider.Entry
}

// githubClassifiedTask は githubTask に新規/更新の分類情報を付加した型。
type githubClassifiedTask struct {
	task  githubTask
	isNew bool // true=新規, false=更新
}

// expandGitHubTasks は Entry スライスを githubTask スライスに展開する純粋関数。
// Entry.Environments が空 → envScope="" (repoレベル) の task 1件
// Entry.Environments が非空 → 各 envScope の task
func expandGitHubTasks(entries []provider.Entry) []githubTask {
	var tasks []githubTask
	for _, e := range entries {
		if len(e.Environments) == 0 {
			tasks = append(tasks, githubTask{envScope: "", entry: e})
		} else {
			for _, env := range e.Environments {
				tasks = append(tasks, githubTask{envScope: env, entry: e})
			}
		}
	}
	return tasks
}

// classifyGitHubTasksByExistence はテスト容易性のため存在確認関数 exists を注入して
// 各タスクを新規/更新に分類する。exists(task) → (存在するか, エラー)
func classifyGitHubTasksByExistence(tasks []githubTask, exists func(t githubTask) (bool, error)) ([]githubClassifiedTask, error) {
	result := make([]githubClassifiedTask, len(tasks))
	for i, t := range tasks {
		found, err := exists(t)
		if err != nil {
			scope := t.envScope
			if scope == "" {
				scope = "repo"
			}
			return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgGitHubExistCheckTaskFail, t.entry.Key, scope), err)
		}
		result[i] = githubClassifiedTask{task: t, isNew: !found}
	}
	return result, nil
}

// classifyGitHubTasks は GitHub API を呼んで各タスクを新規/更新に分類する。
func classifyGitHubTasks(client *http.Client, token, owner, repo string, tasks []githubTask) ([]githubClassifiedTask, error) {
	exists := func(t githubTask) (bool, error) {
		if t.entry.Secret {
			return githubSecretExists(client, token, owner, repo, t.envScope, t.entry.Key)
		}
		return githubVariableExists(client, token, owner, repo, t.envScope, t.entry.Key)
	}
	return classifyGitHubTasksByExistence(tasks, exists)
}

// githubPruneTarget は prune で削除する 1 件を表す。
// secret が true なら Actions Secret、false なら Actions Variable。
type githubPruneTarget struct {
	envScope string // 空ならリポジトリレベル
	name     string
	secret   bool
}

// pruneScopes は prune でスキャンする envScope の一覧を返す純粋関数。
// filterEnvs が空のとき: リポジトリレベル("") を常に含め、tasks に現れる named environment を重複なく加える。
// filterEnvs が非空のとき: リポジトリレベル("") を除外し（指定外環境の変数を誤削除しないため）、
// tasks に現れ、かつ filterEnvs に含まれる named environment のみを加える。
// 定義ファイルに現れない environment はスキャンしない（削除対象の探索範囲を管理対象に限定する）。
func pruneScopes(tasks []githubTask, filterEnvs []string) []string {
	if len(filterEnvs) == 0 {
		// フラグ未指定: 従来通り repo レベルを含む全スコープ
		scopes := []string{""}
		seen := map[string]bool{"": true}
		for _, t := range tasks {
			if !seen[t.envScope] {
				seen[t.envScope] = true
				scopes = append(scopes, t.envScope)
			}
		}
		return scopes
	}
	// --environments 指定時: repo レベル("") を除外し、指定環境のみを対象にする
	filterSet := make(map[string]bool, len(filterEnvs))
	for _, e := range filterEnvs {
		filterSet[e] = true
	}
	var scopes []string
	seen := make(map[string]bool)
	for _, t := range tasks {
		if t.envScope != "" && filterSet[t.envScope] && !seen[t.envScope] {
			seen[t.envScope] = true
			scopes = append(scopes, t.envScope)
		}
	}
	return scopes
}

// undefinedNames は names のうち保持対象でないもの（＝削除対象）を返す純粋関数。
// keep は Options.PruneKeep が返す保持判定（定義済みキー + prune_exclude パターン、大文字小文字非区別）。
func undefinedNames(names []string, keep func(key string) bool) []string {
	var result []string
	for _, n := range names {
		if !keep(n) {
			result = append(result, n)
		}
	}
	return result
}

// collectGitHubPrune は各スコープの Secrets/Variables を列挙し、定義ファイルに無いものを削除対象として返す。
func collectGitHubPrune(client *http.Client, token, owner, repo string, scopes []string, keep func(key string) bool) ([]githubPruneTarget, error) {
	var result []githubPruneTarget
	for _, scope := range scopes {
		secretNames, err := githubListNames(client, token, owner, repo, scope, true)
		if err != nil {
			return nil, err
		}
		for _, n := range undefinedNames(secretNames, keep) {
			result = append(result, githubPruneTarget{envScope: scope, name: n, secret: true})
		}
		varNames, err := githubListNames(client, token, owner, repo, scope, false)
		if err != nil {
			return nil, err
		}
		for _, n := range undefinedNames(varNames, keep) {
			result = append(result, githubPruneTarget{envScope: scope, name: n, secret: false})
		}
	}
	return result, nil
}

// githubListNames は Actions Secrets（secret=true）または Variables（secret=false）の名前一覧を返す。
// per_page=100 でページングし全件を集める。
func githubListNames(client *http.Client, token, owner, repo, envScope string, secret bool) ([]string, error) {
	kind := "variables"
	if secret {
		kind = "secrets"
	}
	var base string
	if envScope == "" {
		base = fmt.Sprintf("%s/repos/%s/%s/actions/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), kind)
	} else {
		base = fmt.Sprintf("%s/repos/%s/%s/environments/%s/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope), kind)
	}

	var names []string
	const perPage = 100
	for page := 1; ; page++ {
		apiURL := fmt.Sprintf("%s?per_page=%d&page=%d", base, perPage, page)
		req, err := http.NewRequest(http.MethodGet, apiURL, nil)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
		}
		setGitHubHeaders(req, token)

		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestFail), err)
		}

		if res.StatusCode != http.StatusOK {
			msg := fmt.Sprintf("HTTP %d", res.StatusCode)
			if detail := parseGitHubErrorBody(res.Body); detail != "" {
				msg += ": " + detail
			}
			res.Body.Close()
			return nil, fmt.Errorf("%s", msg)
		}

		// secrets / variables どちらのレスポンスにも対応する共通デコード
		var resp struct {
			TotalCount int `json:"total_count"`
			Secrets    []struct {
				Name string `json:"name"`
			} `json:"secrets"`
			Variables []struct {
				Name string `json:"name"`
			} `json:"variables"`
		}
		err = json.NewDecoder(res.Body).Decode(&resp)
		res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestFail), err)
		}

		count := 0
		for _, s := range resp.Secrets {
			names = append(names, s.Name)
			count++
		}
		for _, v := range resp.Variables {
			names = append(names, v.Name)
			count++
		}
		// total_count に達したら終了（件数が per_page の倍数のときの空ページ取得を避ける）。
		// count < perPage は total_count が取得できない場合のフォールバック終了条件。
		if count < perPage || (resp.TotalCount > 0 && len(names) >= resp.TotalCount) {
			return names, nil
		}
	}
}

// githubDeletePruneTarget は prune 対象 1 件（Secret または Variable）を削除する。
func githubDeletePruneTarget(client *http.Client, token, owner, repo string, t githubPruneTarget) error {
	kind := "variables"
	if t.secret {
		kind = "secrets"
	}
	var apiURL string
	if t.envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/%s/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), kind, url.PathEscape(t.name))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/%s/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(t.envScope), kind, url.PathEscape(t.name))
	}

	req, err := http.NewRequest(http.MethodDelete, apiURL, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgSendFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return nil
	}

	msg := fmt.Sprintf("HTTP %d", res.StatusCode)
	if detail := parseGitHubErrorBody(res.Body); detail != "" {
		msg += ": " + detail
	}
	return fmt.Errorf("%s", msg)
}

// countGitHubClassified は classified から新規件数・更新件数を返す。
// classified が nil のときは新規=total、更新=0 を返す。
func countGitHubClassified(classified []githubClassifiedTask, total int) (newCount, updateCount int) {
	if classified == nil {
		return total, 0
	}
	for _, c := range classified {
		if c.isNew {
			newCount++
		} else {
			updateCount++
		}
	}
	return
}

// Sync は GitHub Actions への環境変数/シークレット同期を行う。
// github.repos が設定されている場合は複数リポジトリへ順に同期する。
func (g *githubProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return err
	}
	if !opts.DryRun && appCfg.ResolveGitHubToken() == "" && len(appCfg.GitHub.Repos) == 0 {
		return fmt.Errorf("%s", i18n.T(i18n.MsgGitHubTokenMissing))
	}

	// ---- ターゲット解決 ----
	targets, err := appCfg.ResolveGitHubTargets(opts.GitHubRepo)
	if err != nil {
		return err
	}

	// ---- 登録対象を展開 ----
	tasks := expandGitHubTasks(entries)

	client := &http.Client{Timeout: httpTimeout}

	// perTargetClassified はターゲット順に分類結果を保持し、確認・送信フェーズで再利用する。
	// skipped が true のターゲットはトークン未設定のためスキップ済み（複数ターゲット時の失敗集約用）。
	type resolvedTarget struct {
		owner, repo string
		token       string
		classified  []githubClassifiedTask
		prune       []githubPruneTarget // prune 削除対象（opts.Prune が false なら常に空）
		skipped     bool                // トークン未設定により送信をスキップするターゲット
	}
	resolved := make([]resolvedTarget, 0, len(targets))
	pruneKeep := opts.PruneKeep()

	// ---- 各ターゲットについて一覧表示と分類（dry-run も同様）----
	for _, tgt := range targets {
		ownerStr, repoStr, resolveErr := resolveOwnerRepo(tgt, appCfg)
		if resolveErr != nil {
			return resolveErr
		}

		// per-target トークンを決定
		// 単一ターゲット時は即エラー返却。複数ターゲット時は失敗として記録して残りを継続する。
		targetToken := tgt.Token
		if !opts.DryRun && targetToken == "" {
			if len(targets) == 1 {
				return fmt.Errorf("%s", i18n.T(i18n.MsgGitHubTokenMissingRepo, tgt.Name))
			}
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgGitHubTokenSkipRepo, tgt.Name))
			resolved = append(resolved, resolvedTarget{owner: ownerStr, repo: repoStr, skipped: true})
			continue
		}

		// 既存確認して新規/更新を分類（結果を保存して後で再利用し、二重呼び出しを避ける）
		var classified []githubClassifiedTask
		var pruneTargets []githubPruneTarget
		if targetToken != "" {
			cls, err := classifyGitHubTasks(client, targetToken, ownerStr, repoStr, tasks)
			if err == nil {
				classified = cls
			} else {
				// API 失敗時は classified = nil のまま（安全側フォールバック）。
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgGitHubExistingCheckWarn, err))
			}
			// prune 削除対象の収集（一覧取得に失敗した場合は削除をスキップする安全側フォールバック）
			if opts.Prune {
				pt, err := collectGitHubPrune(client, targetToken, ownerStr, repoStr, pruneScopes(tasks, opts.Environments), pruneKeep)
				if err == nil {
					pruneTargets = pt
				} else {
					fmt.Fprint(os.Stderr, i18n.T(i18n.MsgPruneSkipWarn, err))
				}
			}
		}
		resolved = append(resolved, resolvedTarget{owner: ownerStr, repo: repoStr, token: targetToken, classified: classified, prune: pruneTargets})

		// 一覧表示
		fmt.Print(i18n.T(i18n.MsgGitHubTargetRepo, ownerStr, repoStr))
		newCount, updateCount := countGitHubClassified(classified, len(tasks))
		if classified != nil {
			fmt.Print(i18n.T(i18n.MsgEntriesClassified, len(tasks), newCount, updateCount))
		} else {
			fmt.Print(i18n.T(i18n.MsgEntriesCount, len(tasks)))
		}
		for i, t := range tasks {
			kind := "Secret"
			if !t.entry.Secret {
				kind = "Variable"
			}
			scope := t.envScope
			if scope == "" {
				scope = "repo"
			}
			if classified != nil {
				marker, label := "⟳", i18n.T(i18n.MsgLabelUpdate)
				if classified[i].isNew {
					marker, label = "+", i18n.T(i18n.MsgLabelNew)
				}
				fmt.Printf("  %s %s (env: %s, %s) [%s]\n", marker, t.entry.Key, scope, kind, label)
			} else {
				fmt.Printf("  %s (env: %s, %s)\n", t.entry.Key, scope, kind)
			}
		}
		// prune 削除対象の一覧表示
		if len(pruneTargets) > 0 {
			fmt.Print(i18n.T(i18n.MsgPruneEntries, len(pruneTargets)))
			for _, pt := range pruneTargets {
				kind := "Secret"
				if !pt.secret {
					kind = "Variable"
				}
				scope := pt.envScope
				if scope == "" {
					scope = "repo"
				}
				fmt.Printf("  - %s (env: %s, %s) [%s]\n", pt.name, scope, kind, i18n.T(i18n.MsgLabelDelete))
			}
		}
		fmt.Println()
	}

	totalPrune := 0
	for _, r := range resolved {
		totalPrune += len(r.prune)
	}
	if len(tasks) == 0 && totalPrune == 0 {
		fmt.Println(i18n.T(i18n.MsgNoEntries))
		return nil
	}
	if opts.DryRun {
		fmt.Println(i18n.T(i18n.MsgDryRun))
		return nil
	}

	// ---- 確認（更新がある場合、または分類不可の場合）----
	// 一覧表示フェーズで計算した分類結果（resolved[].classified）を再利用し API 二重呼び出しを避ける。
	// skipped のターゲットはスキップ済みのため送信対象件数から除外する。
	// 複数ターゲット時は常に確認（安全側）。単一ターゲット時は更新有無で判定。
	activeResolved := 0
	for _, r := range resolved {
		if !r.skipped {
			activeResolved++
		}
	}
	needsConfirm := false
	if activeResolved > 1 {
		needsConfirm = true
	} else if activeResolved == 1 {
		// 送信可能な単一ターゲット: 保存済み分類を再利用
		for _, r := range resolved {
			if !r.skipped {
				_, updateCount := countGitHubClassified(r.classified, len(tasks))
				needsConfirm = r.classified == nil || updateCount > 0
				break
			}
		}
	}
	// 削除は破壊的操作のため、prune 対象がある場合は必ず確認する
	if totalPrune > 0 {
		needsConfirm = true
		fmt.Print(i18n.T(i18n.MsgPruneConfirmNote, totalPrune))
	}
	if needsConfirm && !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("%s", i18n.T(i18n.MsgNonInteractiveErr))
		}
		if activeResolved > 1 {
			fmt.Print(i18n.T(i18n.MsgGitHubConfirmMulti, activeResolved))
		} else {
			fmt.Print(i18n.T(i18n.MsgGitHubConfirmSingle))
		}
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println(i18n.T(i18n.MsgAborted))
			return nil
		}
	}

	// ---- 各ターゲットへ送信（保存済み分類を再利用して存在確認の二重呼び出しを避ける）----
	// skipped が true のターゲットはトークン未設定のため送信をスキップし、失敗として集計する。
	totalOK, totalNG := 0, 0
	for _, r := range resolved {
		if r.skipped {
			// 一覧表示フェーズで警告済み。失敗件数として集計する。
			totalNG += len(tasks)
			continue
		}
		if activeResolved > 1 {
			fmt.Print(i18n.T(i18n.MsgGitHubRepoSeparator, r.owner, r.repo))
		}
		ok, ng := syncOneGitHubTarget(client, r.token, r.owner, r.repo, tasks, r.classified)
		totalOK += ok
		totalNG += ng
		// prune 削除（upsert 完了後に実行）
		for _, pt := range r.prune {
			if err := githubDeletePruneTarget(client, r.token, r.owner, r.repo, pt); err != nil {
				fmt.Printf("✗ %s -> %s\n", pt.name, err)
				totalNG++
			} else {
				scope := pt.envScope
				if scope == "" {
					scope = "repo"
				}
				fmt.Printf("✓ %s (env: %s) [%s]\n", pt.name, scope, i18n.T(i18n.MsgLabelDelete))
				totalOK++
			}
		}
	}

	if activeResolved > 1 {
		fmt.Print(i18n.T(i18n.MsgTotalCompleted, totalOK, totalNG))
	} else {
		fmt.Print(i18n.T(i18n.MsgCompleted, totalOK, totalNG))
	}
	if totalNG > 0 {
		os.Exit(1)
	}
	return nil
}

// resolveOwnerRepo は GitHubTarget から owner/repo を解決する。
// tgt.Repo が非空ならそれを使い、空なら appCfg 経由で git remote フォールバックを試みる。
func resolveOwnerRepo(tgt config.GitHubTarget, appCfg *config.AppConfig) (owner, repo string, err error) {
	repoStr := tgt.Repo
	if repoStr != "" {
		parts := strings.Split(repoStr, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoFormatInvalid, repoStr))
		}
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
	}
	// 空なら既存の resolveGitHubRepo（環境変数 > config > git remote）で解決
	return resolveGitHubRepo(appCfg)
}

// syncOneGitHubTarget は 1 つの GitHub リポジトリへ tasks を送信し、成功数・失敗数を返す。
// classified は呼び出し元で計算済みの分類結果（nil の場合は variable の存在確認 API にフォールバック）。
// os.Exit は呼ばない。
func syncOneGitHubTarget(client *http.Client, token, owner, repo string, tasks []githubTask, classified []githubClassifiedTask) (okCount, ngCount int) {
	// 公開鍵キャッシュ（envScope ごと）
	type cachedKeyEntry struct {
		keyID string
		key   *[32]byte
	}
	keyCache := map[string]cachedKeyEntry{}

	for i, t := range tasks {
		var sendErr error
		if t.entry.Secret {
			// 公開鍵を取得（envScope ごとにキャッシュ）
			cached, hit := keyCache[t.envScope]
			if !hit {
				keyID, pubKey, e := githubPublicKey(client, token, owner, repo, t.envScope)
				if e != nil {
					fmt.Print(i18n.T(i18n.MsgGitHubPublicKeyFetchFailOut, t.entry.Key, e))
					ngCount++
					continue
				}
				cached = cachedKeyEntry{keyID: keyID, key: pubKey}
				keyCache[t.envScope] = cached
			}
			// 暗号化
			encrypted, e := encryptSecret(t.entry.Value, cached.key)
			if e != nil {
				fmt.Print(i18n.T(i18n.MsgGitHubEncryptFailOut, t.entry.Key, e))
				ngCount++
				continue
			}
			sendErr = githubPutSecret(client, token, owner, repo, t.envScope, t.entry.Key, encrypted, cached.keyID)
		} else {
			// variable: 分類フェーズで取得済みなら classified を再利用し、存在確認 API の二重呼び出しを避ける。
			// classified==nil（分類スキップ）のときだけ存在確認 API にフォールバックする。
			var exists bool
			if classified != nil {
				exists = !classified[i].isNew
			} else {
				var e error
				exists, e = githubVariableExists(client, token, owner, repo, t.envScope, t.entry.Key)
				if e != nil {
					scope := t.envScope
					if scope == "" {
						scope = "repo"
					}
					fmt.Print(i18n.T(i18n.MsgGitHubExistCheckFailOut, t.entry.Key, scope, e))
					ngCount++
					continue
				}
			}
			if exists {
				sendErr = githubUpdateVariable(client, token, owner, repo, t.envScope, t.entry.Key, t.entry.Value)
			} else {
				sendErr = githubCreateVariable(client, token, owner, repo, t.envScope, t.entry.Key, t.entry.Value)
			}
		}

		if sendErr != nil {
			fmt.Printf("✗ %s -> %s\n", t.entry.Key, sendErr)
			ngCount++
		} else {
			kind := "Secret"
			if !t.entry.Secret {
				kind = "Variable"
			}
			scope := t.envScope
			if scope == "" {
				scope = "repo"
			}
			fmt.Printf("✓ %s (env: %s, %s)\n", t.entry.Key, scope, kind)
			okCount++
		}
	}
	return okCount, ngCount
}

// resolveGitHubRepo は config (環境変数 > config ファイル) または git remote から owner/repo を解決する。
func resolveGitHubRepo(appCfg *config.AppConfig) (owner, repo string, err error) {
	repoEnv := strings.TrimSpace(appCfg.ResolveGitHubRepo())
	if repoEnv != "" {
		parts := strings.Split(repoEnv, "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoEnvInvalid))
		}
		owner := strings.TrimSpace(parts[0])
		repo := strings.TrimSpace(parts[1])
		if owner == "" || repo == "" {
			return "", "", fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoEnvInvalid))
		}
		return owner, repo, nil
	}

	// git remote から取得
	o, r, ok := repoFromGitRemote()
	if !ok {
		return "", "", fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoRequired))
	}
	return o, r, nil
}

// repoFromGitRemote は git remote get-url origin でリポジトリを解決する。
func repoFromGitRemote() (owner, repo string, ok bool) {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", false
	}
	rawURL := strings.TrimSpace(string(out))
	return parseGitHubRemoteURL(rawURL)
}

// parseGitHubRemoteURL は git remote の URL から GitHub の owner/repo を抽出する。
// SSH 形式: git@github.com:owner/repo.git
// HTTPS 形式: https://github.com/owner/repo.git
func parseGitHubRemoteURL(rawURL string) (owner, repo string, ok bool) {
	if rawURL == "" {
		return "", "", false
	}

	var path string

	switch {
	case strings.HasPrefix(rawURL, "git@github.com:"):
		// SCP 風 SSH 形式: git@github.com:owner/repo.git
		path = strings.TrimPrefix(rawURL, "git@github.com:")
	case strings.HasPrefix(rawURL, "ssh://git@github.com/"):
		// ssh:// 形式: ssh://git@github.com/owner/repo.git
		path = strings.TrimPrefix(rawURL, "ssh://git@github.com/")
	case strings.HasPrefix(rawURL, "https://github.com/"):
		// HTTPS 形式: https://github.com/owner/repo.git
		path = strings.TrimPrefix(rawURL, "https://github.com/")
	default:
		return "", "", false
	}

	// 末尾の改行・スラッシュ・.git を除去
	path = strings.TrimSpace(path)
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")

	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// encryptSecret は値を公開鍵で sealed box 暗号化し base64 エンコードした文字列を返す。
func encryptSecret(value string, pubKey *[32]byte) (string, error) {
	encrypted, err := box.SealAnonymous(nil, []byte(value), pubKey, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("%s: %w", i18n.T(i18n.MsgSealedBoxEncryptFail), err)
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

// githubPublicKey は GitHub Actions の公開鍵を取得する。
// envScope が空の場合はリポジトリレベル、それ以外は Environment スコープ。
func githubPublicKey(client *http.Client, token, owner, repo, envScope string) (keyID string, key *[32]byte, err error) {
	var apiURL string
	if envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/secrets/public-key",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/secrets/public-key",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope))
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgPublicKeyFetchFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseGitHubErrorBody(res.Body); detail != "" {
			msg += ": " + detail
		}
		return "", nil, fmt.Errorf("%s: %s", i18n.T(i18n.MsgPublicKeyFetchFail), msg)
	}

	var keyResp struct {
		KeyID string `json:"key_id"`
		Key   string `json:"key"`
	}
	if err := json.NewDecoder(res.Body).Decode(&keyResp); err != nil {
		return "", nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgPublicKeyParseFail), err)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(keyResp.Key)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", i18n.T(i18n.MsgPublicKeyDecodeFail), err)
	}
	if len(keyBytes) != 32 {
		return "", nil, fmt.Errorf("%s", i18n.T(i18n.MsgPublicKeyLengthInvalid, len(keyBytes)))
	}

	var pubKey [32]byte
	copy(pubKey[:], keyBytes)
	return keyResp.KeyID, &pubKey, nil
}

// githubPutSecret は GitHub Actions のシークレットを作成/更新する（upsert）。
func githubPutSecret(client *http.Client, token, owner, repo, envScope, name, encryptedValue, keyID string) error {
	var apiURL string
	if envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/secrets/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(name))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/secrets/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope), url.PathEscape(name))
	}

	body, _ := json.Marshal(map[string]string{
		"encrypted_value": encryptedValue,
		"key_id":          keyID,
	})

	req, err := http.NewRequest(http.MethodPut, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgSendFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return nil
	}

	msg := fmt.Sprintf("HTTP %d", res.StatusCode)
	if detail := parseGitHubErrorBody(res.Body); detail != "" {
		msg += ": " + detail
	}
	return fmt.Errorf("%s", msg)
}

// githubSecretExists は GitHub Actions のシークレットが存在するかを確認する。
func githubSecretExists(client *http.Client, token, owner, repo, envScope, name string) (bool, error) {
	var apiURL string
	if envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/secrets/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(name))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/secrets/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope), url.PathEscape(name))
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return false, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusOK {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return true, nil
	}
	if res.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return false, nil
	}

	msg := fmt.Sprintf("HTTP %d", res.StatusCode)
	if detail := parseGitHubErrorBody(res.Body); detail != "" {
		msg += ": " + detail
	}
	return false, fmt.Errorf("%s", i18n.T(i18n.MsgSecretExistCheckFail, msg))
}

// githubVariableExists は GitHub Actions の変数が存在するかを確認する。
func githubVariableExists(client *http.Client, token, owner, repo, envScope, name string) (bool, error) {
	var apiURL string
	if envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/variables/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(name))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/variables/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope), url.PathEscape(name))
	}

	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return false, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusOK {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return true, nil
	}
	if res.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return false, nil
	}

	msg := fmt.Sprintf("HTTP %d", res.StatusCode)
	if detail := parseGitHubErrorBody(res.Body); detail != "" {
		msg += ": " + detail
	}
	return false, fmt.Errorf("%s", i18n.T(i18n.MsgVariableExistCheckFail, msg))
}

// githubCreateVariable は GitHub Actions の変数を新規作成する。
func githubCreateVariable(client *http.Client, token, owner, repo, envScope, name, value string) error {
	var apiURL string
	if envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/variables",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/variables",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope))
	}

	body, _ := json.Marshal(map[string]string{
		"name":  name,
		"value": value,
	})

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgSendFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusCreated {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return nil
	}

	msg := fmt.Sprintf("HTTP %d", res.StatusCode)
	if detail := parseGitHubErrorBody(res.Body); detail != "" {
		msg += ": " + detail
	}
	return fmt.Errorf("%s", msg)
}

// githubUpdateVariable は GitHub Actions の既存変数を更新する。
func githubUpdateVariable(client *http.Client, token, owner, repo, envScope, name, value string) error {
	var apiURL string
	if envScope == "" {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/actions/variables/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(name))
	} else {
		apiURL = fmt.Sprintf("%s/repos/%s/%s/environments/%s/variables/%s",
			githubAPIBase, url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(envScope), url.PathEscape(name))
	}

	body, _ := json.Marshal(map[string]string{
		"name":  name,
		"value": value,
	})

	req, err := http.NewRequest(http.MethodPatch, apiURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgRequestCreateFail), err)
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.MsgSendFail), err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, res.Body) //nolint:errcheck // drain で接続を再利用可能にする
		return nil
	}

	msg := fmt.Sprintf("HTTP %d", res.StatusCode)
	if detail := parseGitHubErrorBody(res.Body); detail != "" {
		msg += ": " + detail
	}
	return fmt.Errorf("%s", msg)
}

// setGitHubHeaders は GitHub API 共通ヘッダを設定する。
func setGitHubHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
}

// parseGitHubErrorBody は GitHub のエラーレスポンス本文からメッセージを取り出す。
func parseGitHubErrorBody(r io.Reader) string {
	data, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return ""
	}
	return body.Message
}
