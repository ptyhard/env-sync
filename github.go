package main

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

	"golang.org/x/crypto/nacl/box"
)

// githubAPIBase は GitHub REST API のベース URL。テストで httptest.Server を
// 指す差し替えができるよう var にしている。
var githubAPIBase = "https://api.github.com"

func init() {
	registerProvider("github", func() Provider { return &githubProvider{} })
}

// githubProvider は GitHub Actions への同期を担当する Provider 実装。
type githubProvider struct{}

func (g *githubProvider) Name() string { return "github" }

// githubTask は GitHub Actions に登録する 1 件のタスク情報を表す。
// envScope が空のときはリポジトリレベル、それ以外は named environment スコープ。
type githubTask struct {
	envScope string
	entry    Entry
}

// expandGitHubTasks は Entry スライスを githubTask スライスに展開する純粋関数。
// Entry.Environments が空 → envScope="" (repoレベル) の task 1件
// Entry.Environments が非空 → 各 envScope の task
func expandGitHubTasks(entries []Entry) []githubTask {
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

// Sync は GitHub Actions への環境変数/シークレット同期を行う。
func (g *githubProvider) Sync(opts options, entries []Entry) error {
	token := os.Getenv("GITHUB_TOKEN")
	if !opts.dryRun && token == "" {
		return die("GITHUB_TOKEN が未設定です")
	}

	// ---- リポジトリ解決 ----
	owner, repo, err := resolveGitHubRepo()
	if err != nil {
		return err
	}

	// ---- 登録対象を展開 ----
	tasks := expandGitHubTasks(entries)

	// ---- 一覧表示 ----
	fmt.Printf("対象リポジトリ: %s/%s\n", owner, repo)
	fmt.Printf("登録対象 %d 件:\n", len(tasks))
	for _, t := range tasks {
		kind := "Secret"
		if !t.entry.Secret {
			kind = "Variable"
		}
		scope := t.envScope
		if scope == "" {
			scope = "repo"
		}
		fmt.Printf("  %s (env: %s, %s)\n", t.entry.Key, scope, kind)
	}
	fmt.Println()

	if len(tasks) == 0 {
		fmt.Println("登録対象がありません")
		return nil
	}
	if opts.dryRun {
		fmt.Println("[dry-run] 送信しません")
		return nil
	}

	// ---- 確認 ----
	if !opts.yes {
		if !isTTY(os.Stdin) {
			return die("対話できない環境です。確認をスキップするには --yes を付けてください")
		}
		fmt.Print("上記を GitHub に登録します。続行しますか? (y/N) ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("中止しました")
			return nil
		}
	}

	// ---- 送信 ----
	client := &http.Client{}
	okCount, ngCount := 0, 0

	// 公開鍵キャッシュ（envScope ごと）
	type cachedKeyEntry struct {
		keyID string
		key   *[32]byte
	}
	keyCache := map[string]cachedKeyEntry{}

	for _, t := range tasks {
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
			// variable: GET で存在確認 → POST or PATCH
			exists, e := githubVariableExists(client, token, owner, repo, t.envScope, t.entry.Key)
			if e != nil {
				fmt.Printf("✗ %s -> 存在確認失敗: %s\n", t.entry.Key, e)
				ngCount++
				continue
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

	fmt.Printf("\n完了: 成功 %d / 失敗 %d\n", okCount, ngCount)
	if ngCount > 0 {
		os.Exit(1)
	}
	return nil
}

// resolveGitHubRepo は GITHUB_REPO 環境変数または git remote から owner/repo を解決する。
func resolveGitHubRepo() (owner, repo string, err error) {
	repoEnv := strings.TrimSpace(os.Getenv("GITHUB_REPO"))
	if repoEnv != "" {
		parts := strings.Split(repoEnv, "/")
		if len(parts) != 2 {
			return "", "", die("GITHUB_REPO の形式が不正です（owner/repo 形式で指定してください）")
		}
		owner := strings.TrimSpace(parts[0])
		repo := strings.TrimSpace(parts[1])
		if owner == "" || repo == "" {
			return "", "", die("GITHUB_REPO の形式が不正です（owner/repo 形式で指定してください）")
		}
		return owner, repo, nil
	}

	// git remote から取得
	o, r, ok := repoFromGitRemote()
	if !ok {
		return "", "", die("GITHUB_REPO を指定してください（git remote origin が GitHub でないか、git が使えません）")
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
