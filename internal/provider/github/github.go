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
			return nil, fmt.Errorf("%s (env: %s): 存在確認失敗: %w", t.entry.Key, scope, err)
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
		return fmt.Errorf("GITHUB_TOKEN が未設定です（環境変数 GITHUB_TOKEN または config ファイルの github.token で指定してください）")
	}

	// ---- ターゲット解決 ----
	targets, err := appCfg.ResolveGitHubTargets(opts.GitHubRepo)
	if err != nil {
		return err
	}

	// 複数 repos 定義時は各 target の repo が必須（git remote フォールバックなし）
	if len(targets) > 1 {
		for _, tgt := range targets {
			if tgt.Repo == "" {
				return fmt.Errorf("GitHub リポジトリ %q の repo が設定されていません", tgt.Name)
			}
		}
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
		skipped     bool // トークン未設定により送信をスキップするターゲット
	}
	resolved := make([]resolvedTarget, 0, len(targets))

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
				return fmt.Errorf("GITHUB_TOKEN が未設定です（リポジトリ %q: 環境変数 GITHUB_TOKEN または config ファイルの token で指定してください）", tgt.Name)
			}
			fmt.Fprintf(os.Stderr, "✗ リポジトリ %q: GITHUB_TOKEN が未設定です（このターゲットをスキップして残りを継続します）\n", tgt.Name)
			resolved = append(resolved, resolvedTarget{owner: ownerStr, repo: repoStr, skipped: true})
			continue
		}

		// 既存確認して新規/更新を分類（結果を保存して後で再利用し、二重呼び出しを避ける）
		var classified []githubClassifiedTask
		if targetToken != "" {
			cls, err := classifyGitHubTasks(client, targetToken, ownerStr, repoStr, tasks)
			if err == nil {
				classified = cls
			} else {
				// API 失敗時は classified = nil のまま（安全側フォールバック）。
				fmt.Fprintf(os.Stderr, "警告: 既存の存在確認に失敗したため新規/更新の分類をスキップします: %s\n", err)
			}
		}
		resolved = append(resolved, resolvedTarget{owner: ownerStr, repo: repoStr, token: targetToken, classified: classified})

		// 一覧表示
		fmt.Printf("対象リポジトリ: %s/%s\n", ownerStr, repoStr)
		newCount, updateCount := countGitHubClassified(classified, len(tasks))
		if classified != nil {
			fmt.Printf("登録対象 %d 件 (新規 %d 件 / 更新 %d 件):\n", len(tasks), newCount, updateCount)
		} else {
			fmt.Printf("登録対象 %d 件:\n", len(tasks))
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
				marker, label := "⟳", "更新"
				if classified[i].isNew {
					marker, label = "+", "新規"
				}
				fmt.Printf("  %s %s (env: %s, %s) [%s]\n", marker, t.entry.Key, scope, kind, label)
			} else {
				fmt.Printf("  %s (env: %s, %s)\n", t.entry.Key, scope, kind)
			}
		}
		fmt.Println()
	}

	if len(tasks) == 0 {
		fmt.Println("登録対象がありません")
		return nil
	}
	if opts.DryRun {
		fmt.Println("[dry-run] 送信しません")
		return nil
	}

	// ---- 確認（更新がある場合、または分類不可の場合）----
	// 一覧表示フェーズで計算した分類結果（resolved[].classified）を再利用し API 二重呼び出しを避ける。
	// 複数ターゲット時は常に確認（安全側）。単一ターゲット時は更新有無で判定。
	needsConfirm := false
	if len(resolved) > 1 {
		needsConfirm = true
	} else {
		// 単一ターゲット: 保存済み分類を再利用
		classified := resolved[0].classified
		_, updateCount := countGitHubClassified(classified, len(tasks))
		needsConfirm = classified == nil || updateCount > 0
	}
	if needsConfirm && !opts.Yes {
		if !config.IsTTY(os.Stdin) {
			return fmt.Errorf("対話できない環境です。確認をスキップするには --yes を付けてください")
		}
		if len(resolved) > 1 {
			fmt.Printf("上記を GitHub の %d リポジトリに登録します（既存は上書き）。続行しますか? (y/N) ", len(resolved))
		} else {
			fmt.Print("上記を GitHub に登録します。続行しますか? (y/N) ")
		}
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("中止しました")
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
		if len(resolved) > 1 {
			fmt.Printf("\n--- リポジトリ: %s/%s ---\n", r.owner, r.repo)
		}
		ok, ng := syncOneGitHubTarget(client, r.token, r.owner, r.repo, tasks, r.classified)
		totalOK += ok
		totalNG += ng
	}

	if len(resolved) > 1 {
		fmt.Printf("\n全体完了: 成功 %d / 失敗 %d\n", totalOK, totalNG)
	} else {
		fmt.Printf("\n完了: 成功 %d / 失敗 %d\n", totalOK, totalNG)
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
			return "", "", fmt.Errorf("リポジトリの形式が不正です（owner/repo 形式で指定してください）: %q", repoStr)
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
					fmt.Printf("✗ %s -> 公開鍵取得失敗: %s\n", t.entry.Key, e)
					ngCount++
					continue
				}
				cached = cachedKeyEntry{keyID: keyID, key: pubKey}
				keyCache[t.envScope] = cached
			}
			// 暗号化
			encrypted, e := encryptSecret(t.entry.Value, cached.key)
			if e != nil {
				fmt.Printf("✗ %s -> 暗号化失敗: %s\n", t.entry.Key, e)
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
					fmt.Printf("✗ %s (env: %s) -> 存在確認失敗: %s\n", t.entry.Key, scope, e)
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
			return "", "", fmt.Errorf("GITHUB_REPO の形式が不正です（owner/repo 形式で指定してください）")
		}
		owner := strings.TrimSpace(parts[0])
		repo := strings.TrimSpace(parts[1])
		if owner == "" || repo == "" {
			return "", "", fmt.Errorf("GITHUB_REPO の形式が不正です（owner/repo 形式で指定してください）")
		}
		return owner, repo, nil
	}

	// git remote から取得
	o, r, ok := repoFromGitRemote()
	if !ok {
		return "", "", fmt.Errorf("GITHUB_REPO を指定してください（git remote origin が GitHub でないか、git が使えません）")
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
		return "", fmt.Errorf("sealed box 暗号化失敗: %w", err)
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
		return "", nil, fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("公開鍵取得失敗: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", res.StatusCode)
		if detail := parseGitHubErrorBody(res.Body); detail != "" {
			msg += ": " + detail
		}
		return "", nil, fmt.Errorf("公開鍵取得失敗: %s", msg)
	}

	var keyResp struct {
		KeyID string `json:"key_id"`
		Key   string `json:"key"`
	}
	if err := json.NewDecoder(res.Body).Decode(&keyResp); err != nil {
		return "", nil, fmt.Errorf("公開鍵レスポンスのパース失敗: %w", err)
	}

	keyBytes, err := base64.StdEncoding.DecodeString(keyResp.Key)
	if err != nil {
		return "", nil, fmt.Errorf("公開鍵の base64 デコード失敗: %w", err)
	}
	if len(keyBytes) != 32 {
		return "", nil, fmt.Errorf("公開鍵の長さが不正です（%d バイト、32 バイト必要）", len(keyBytes))
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
		return fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("送信失敗: %w", err)
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
		return false, fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("リクエスト失敗: %w", err)
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
	return false, fmt.Errorf("シークレットの存在確認失敗: %s", msg)
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
		return false, fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	setGitHubHeaders(req, token)

	res, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("リクエスト失敗: %w", err)
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
	return false, fmt.Errorf("変数の存在確認失敗: %s", msg)
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
		return fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("送信失敗: %w", err)
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
		return fmt.Errorf("リクエスト生成失敗: %w", err)
	}
	setGitHubHeaders(req, token)
	req.Header.Set("Content-Type", "application/json")

	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("送信失敗: %w", err)
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
