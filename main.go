// Command env-sync は、定義ファイル(env-sync.yaml)で宣言した環境変数を
// Vercel または GitHub Actions へ一括登録(同期)する。
//
// 値は定義ファイルには書かず .env(.production) から取得する。
//
// 使い方:
//
//	VERCEL_TOKEN=xxxxx env-sync --env .env.production
//	GITHUB_TOKEN=xxxxx env-sync --provider github --env .env.production
//
// 必須 (Vercel):
//
//	VERCEL_TOKEN        Vercel のアクセストークン (https://vercel.com/account/tokens)
//	VERCEL_PROJECT_ID   プロジェクト ID。未指定なら .vercel/project.json から自動取得
//
// 任意 (Vercel):
//
//	VERCEL_TEAM_ID      チーム(Org) ID。未指定なら .vercel/project.json の orgId
//
// 必須 (GitHub):
//
//	GITHUB_TOKEN        GitHub のアクセストークン（dry-run 時は不要）
//	GITHUB_REPO         owner/repo 形式。未指定なら git remote から取得
//
// オプション:
//
//	--provider vercel|github  同期先（デフォルト vercel）
//	--env  <file>             値を読む env ファイル（デフォルト .env）
//	--def  <file>             type/target 定義 YAML（デフォルト env-sync.yaml）
//	--dry-run                 実際には送信せず、登録予定の key/type/target だけ表示（値は出さない）
//	--yes, -y                 送信前の確認をスキップ
//	--github-env <name>       GitHub Actions の Environment スコープ（未指定はリポジトリレベル）
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
	"sort"
	"strings"

	"golang.org/x/crypto/nacl/box"
	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// ldflags で注入するバージョン情報。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const apiBase = "https://api.vercel.com"

// githubAPIBase は GitHub REST API のベース URL。テストで httptest.Server を
// 指す差し替えができるよう var にしている。
var githubAPIBase = "https://api.github.com"

var validTypes = map[string]bool{
	"plain":     true,
	"encrypted": true,
	"sensitive": true,
}

var validTargets = map[string]bool{
	"production":  true,
	"preview":     true,
	"development": true,
}

var validKinds = map[string]bool{
	"secret":   true,
	"variable": true,
}

// options はコマンドラインフラグの値を保持する。
type options struct {
	env       string
	def       string
	dryRun    bool
	yes       bool
	provider  string
	githubEnv string
}

// item は Vercel へ送信する 1 件の環境変数を表す。
type item struct {
	Key    string   `json:"key"`
	Value  string   `json:"value"`
	Type   string   `json:"type"`
	Target []string `json:"target"`
}

// varConf は定義 YAML の variables 配下 1 件分の設定。
type varConf struct {
	Type   string      `yaml:"type"`
	Target interface{} `yaml:"target"`
	Kind   string      `yaml:"kind"`
}

// definition は定義 YAML 全体の構造。
type definition struct {
	Defaults struct {
		Type   string      `yaml:"type"`
		Target interface{} `yaml:"target"`
		Kind   string      `yaml:"kind"`
	} `yaml:"defaults"`
	Variables map[string]varConf `yaml:"variables"`
}

// projectJSON は .vercel/project.json の必要フィールド。
type projectJSON struct {
	ProjectID string `json:"projectId"`
	OrgID     string `json:"orgId"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "エラー: %s\n", err)
		os.Exit(1)
	}
}

func die(format string, a ...interface{}) error {
	return fmt.Errorf(format, a...)
}

func run() error {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "init" {
		return runInit(args[1:])
	}

	opts := parseFlags(args)

	// ---- 入力読み込み ----
	if !fileExists(opts.env) {
		return die("env ファイルが見つかりません: %s", opts.env)
	}
	if !fileExists(opts.def) {
		return die("定義ファイルが見つかりません: %s", opts.def)
	}

	envText, err := os.ReadFile(opts.env)
	if err != nil {
		return die("env ファイルの読み込みに失敗: %s", err)
	}
	envVars := parseDotenv(string(envText))

	defText, err := os.ReadFile(opts.def)
	if err != nil {
		return die("定義ファイルの読み込みに失敗: %s", err)
	}
	var def definition
	if err := yaml.Unmarshal(defText, &def); err != nil {
		return die("定義ファイルの YAML パースに失敗: %s", err)
	}

	// ---- 整合性チェック（provider 共通） ----
	defKeys := sortedKeys(def.Variables)
	defKeySet := make(map[string]bool, len(defKeys))
	for _, k := range defKeys {
		defKeySet[k] = true
	}
	for _, k := range defKeys {
		if _, ok := envVars[k]; !ok {
			fmt.Fprintf(os.Stderr, "⚠ %s: 定義にあるが %s に値が無いためスキップ\n", k, opts.env)
		}
	}
	for _, k := range sortedStrKeys(envVars) {
		if !defKeySet[k] {
			fmt.Fprintf(os.Stderr, "⚠ %s: %s にあるが定義に無いためスキップ\n", k, opts.env)
		}
	}

	// ---- provider で分岐 ----
	switch opts.provider {
	case "github":
		return syncGitHub(opts, envVars, def, defKeys)
	default:
		return syncVercel(opts, envVars, def, defKeys)
	}
}

// syncVercel は Vercel への環境変数同期を行う（既存ロジック）。
func syncVercel(opts options, envVars map[string]string, def definition, defKeys []string) error {
	defaultType := def.Defaults.Type
	if defaultType == "" {
		defaultType = "sensitive"
	}
	defaultTarget := normalizeTarget(def.Defaults.Target)
	if len(defaultTarget) == 0 {
		defaultTarget = []string{"production", "preview"}
	}

	// ---- 認証情報 / プロジェクト ----
	token := os.Getenv("VERCEL_TOKEN")
	projectID := os.Getenv("VERCEL_PROJECT_ID")
	teamID := os.Getenv("VERCEL_TEAM_ID")
	if projectID == "" && fileExists(".vercel/project.json") {
		pjText, err := os.ReadFile(".vercel/project.json")
		if err != nil {
			return die(".vercel/project.json の読み込みに失敗: %s", err)
		}
		var pj projectJSON
		if err := json.Unmarshal(pjText, &pj); err != nil {
			return die(".vercel/project.json の JSON パースに失敗: %s", err)
		}
		projectID = pj.ProjectID
		if teamID == "" {
			teamID = pj.OrgID
		}
	}
	if !opts.dryRun && token == "" {
		return die("VERCEL_TOKEN が未設定です")
	}
	if projectID == "" {
		return die("VERCEL_PROJECT_ID が未設定で .vercel/project.json もありません（先に vercel link するか指定してください）")
	}

	// ---- 登録対象を組み立て ----
	var items []item
	for _, key := range defKeys {
		val, ok := envVars[key]
		if !ok {
			continue
		}
		conf := def.Variables[key]
		typ := conf.Type
		if typ == "" {
			typ = defaultType
		}
		target := normalizeTarget(conf.Target)
		if len(target) == 0 {
			target = defaultTarget
		}
		if !validTypes[typ] {
			return die("%s: 不正な type %q（%s）", key, typ, strings.Join(sortedSet(validTypes), " / "))
		}
		for _, t := range target {
			if !validTargets[t] {
				return die("%s: 不正な target %q（%s）", key, t, strings.Join(sortedSet(validTargets), " / "))
			}
		}
		items = append(items, item{Key: key, Value: val, Type: typ, Target: target})
	}

	// ---- 登録対象を一覧表示 ----
	fmt.Printf("対象プロジェクト: %s  (env: %s, def: %s)\n", projectID, opts.env, opts.def)
	fmt.Printf("登録対象 %d 件 (既存は upsert で上書き):\n", len(items))
	for _, it := range items {
		tj, _ := json.Marshal(it.Target)
		fmt.Printf("  %s (%s) -> %s\n", it.Key, it.Type, string(tj))
	}
	fmt.Println()

	if len(items) == 0 {
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
		fmt.Print("上記を Vercel に登録します（既存は上書き）。続行しますか? (y/N) ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		ans := strings.ToLower(strings.TrimSpace(line))
		if ans != "y" && ans != "yes" {
			fmt.Println("中止しました")
			return nil
		}
	}

	// ---- 送信 ----
	u, _ := url.Parse(fmt.Sprintf("%s/v10/projects/%s/env", apiBase, projectID))
	q := u.Query()
	q.Set("upsert", "true")
	if teamID != "" {
		q.Set("teamId", teamID)
	}
	u.RawQuery = q.Encode()

	client := &http.Client{}
	ok, ng := 0, 0
	for _, it := range items {
		body, _ := json.Marshal(it)
		req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(body))
		if err != nil {
			fmt.Printf("✗ %s -> リクエスト生成失敗: %s\n", it.Key, err)
			ng++
			continue
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		res, err := client.Do(req)
		if err != nil {
			fmt.Printf("✗ %s -> 送信失敗: %s\n", it.Key, err)
			ng++
			continue
		}
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			fmt.Printf("✓ %s (%s)\n", it.Key, it.Type)
			ok++
		} else {
			msg := fmt.Sprintf("HTTP %d", res.StatusCode)
			if detail := parseErrorBody(res.Body); detail != "" {
				msg += ": " + detail
			}
			fmt.Printf("✗ %s -> %s\n", it.Key, msg)
			ng++
		}
		res.Body.Close()
	}

	fmt.Printf("\n完了: 成功 %d / 失敗 %d\n", ok, ng)
	if ng > 0 {
		os.Exit(1)
	}
	return nil
}

// githubItem は GitHub Actions に登録する 1 件の変数/シークレット情報を表す。
type githubItem struct {
	Key   string
	Value string
	Kind  string // "secret" or "variable"
}

// syncGitHub は GitHub Actions への環境変数/シークレット同期を行う。
func syncGitHub(opts options, envVars map[string]string, def definition, defKeys []string) error {
	token := os.Getenv("GITHUB_TOKEN")
	if !opts.dryRun && token == "" {
		return die("GITHUB_TOKEN が未設定です")
	}

	// ---- リポジトリ解決 ----
	owner, repo, err := resolveGitHubRepo()
	if err != nil {
		return err
	}

	// ---- defaults の kind ----
	defaultKind := def.Defaults.Kind
	if defaultKind == "" {
		defaultKind = "secret"
	}

	// ---- 登録対象を組み立て ----
	var items []githubItem
	for _, key := range defKeys {
		val, ok := envVars[key]
		if !ok {
			continue
		}
		conf := def.Variables[key]
		kind := conf.Kind
		if kind == "" {
			kind = defaultKind
		}
		if !validKinds[kind] {
			return die("%s: 不正な kind %q（secret / variable）", key, kind)
		}
		items = append(items, githubItem{Key: key, Value: val, Kind: kind})
	}

	// ---- 一覧表示 ----
	envScope := opts.githubEnv
	scopeLabel := "リポジトリ"
	if envScope != "" {
		scopeLabel = envScope
	}
	fmt.Printf("対象リポジトリ: %s/%s (env スコープ: %s)\n", owner, repo, scopeLabel)
	fmt.Printf("登録対象 %d 件:\n", len(items))
	for _, it := range items {
		fmt.Printf("  %s (%s)\n", it.Key, it.Kind)
	}
	fmt.Println()

	if len(items) == 0 {
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

	// 公開鍵キャッシュ（secret 用）
	var cachedKeyID string
	var cachedKey *[32]byte

	for _, it := range items {
		var sendErr error
		if it.Kind == "secret" {
			// 公開鍵を取得（キャッシュ）
			if cachedKey == nil {
				keyID, pubKey, e := githubPublicKey(client, token, owner, repo, envScope)
				if e != nil {
					fmt.Printf("✗ %s -> 公開鍵取得失敗: %s\n", it.Key, e)
					ngCount++
					continue
				}
				cachedKeyID = keyID
				cachedKey = pubKey
			}
			// 暗号化
			encrypted, e := encryptSecret(it.Value, cachedKey)
			if e != nil {
				fmt.Printf("✗ %s -> 暗号化失敗: %s\n", it.Key, e)
				ngCount++
				continue
			}
			sendErr = githubPutSecret(client, token, owner, repo, envScope, it.Key, encrypted, cachedKeyID)
		} else {
			// variable: GET で存在確認 → POST or PATCH
			exists, e := githubVariableExists(client, token, owner, repo, envScope, it.Key)
			if e != nil {
				fmt.Printf("✗ %s -> 存在確認失敗: %s\n", it.Key, e)
				ngCount++
				continue
			}
			if exists {
				sendErr = githubUpdateVariable(client, token, owner, repo, envScope, it.Key, it.Value)
			} else {
				sendErr = githubCreateVariable(client, token, owner, repo, envScope, it.Key, it.Value)
			}
		}

		if sendErr != nil {
			fmt.Printf("✗ %s -> %s\n", it.Key, sendErr)
			ngCount++
		} else {
			fmt.Printf("✓ %s (%s)\n", it.Key, it.Kind)
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
		return true, nil
	}
	if res.StatusCode == http.StatusNotFound {
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

// parseFlags はコマンドライン引数を解析する。flag パッケージは特殊な
// 短縮形 (-y) と長形 (--yes) の両立や --dry-run の扱いが煩雑なため手で処理する。
func parseFlags(argv []string) options {
	opts := options{env: ".env", def: "env-sync.yaml", provider: "vercel"}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		next := func() string {
			i++
			if i >= len(argv) {
				fmt.Fprintf(os.Stderr, "エラー: %s には値が必要です\n", arg)
				os.Exit(1)
			}
			return argv[i]
		}
		switch {
		case arg == "--env" || arg == "-env":
			opts.env = next()
		case strings.HasPrefix(arg, "--env="):
			opts.env = strings.TrimPrefix(arg, "--env=")
		case arg == "--def" || arg == "-def":
			opts.def = next()
		case strings.HasPrefix(arg, "--def="):
			opts.def = strings.TrimPrefix(arg, "--def=")
		case arg == "--dry-run" || arg == "-dry-run":
			opts.dryRun = true
		case arg == "--yes" || arg == "-yes" || arg == "-y":
			opts.yes = true
		case arg == "--version" || arg == "-version":
			fmt.Printf("env-sync version %s (commit: %s, built: %s)\n", version, commit, date)
			os.Exit(0)
		case arg == "--provider" || arg == "-provider":
			v := next()
			if v != "vercel" && v != "github" {
				fmt.Fprintf(os.Stderr, "エラー: --provider には vercel または github を指定してください\n")
				os.Exit(1)
			}
			opts.provider = v
		case strings.HasPrefix(arg, "--provider="):
			v := strings.TrimPrefix(arg, "--provider=")
			if v != "vercel" && v != "github" {
				fmt.Fprintf(os.Stderr, "エラー: --provider には vercel または github を指定してください\n")
				os.Exit(1)
			}
			opts.provider = v
		case arg == "--github-env" || arg == "-github-env":
			opts.githubEnv = next()
		case strings.HasPrefix(arg, "--github-env="):
			opts.githubEnv = strings.TrimPrefix(arg, "--github-env=")
		case arg == "-h" || arg == "--help":
			printUsage()
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "エラー: 不明な引数: %s\n", arg)
			printUsage()
			os.Exit(1)
		}
	}
	return opts
}

func printUsage() {
	fmt.Fprint(os.Stderr, `env-sync - 定義ファイルで宣言した環境変数を Vercel または GitHub Actions へ一括登録(同期)する

サブコマンド:
  init   .env から env-sync.yaml の雛形を生成する

使い方:
  VERCEL_TOKEN=xxxxx env-sync [オプション]
  GITHUB_TOKEN=xxxxx env-sync --provider github [オプション]
  env-sync init [--env <file>] [--def <file>] [--force]

オプション（同期）:
  --provider vercel|github  同期先（デフォルト vercel）
  --env <file>              値を読む env ファイル（デフォルト .env）
  --def <file>              type/target 定義 YAML（デフォルト env-sync.yaml）
  --dry-run                 送信せず登録予定の key/type/target だけ表示
  --yes, -y                 送信前の確認をスキップ
  --github-env <name>       GitHub Actions の Environment スコープ（未指定はリポジトリレベル）
  --version                 バージョン情報を表示して終了
  -h, --help                このヘルプを表示

オプション（init）:
  --env <file>   読み込む env ファイル（デフォルト .env）
  --def <file>   出力する YAML ファイル（デフォルト env-sync.yaml）
  --force        既存の def ファイルを上書きする

環境変数（Vercel）:
  VERCEL_TOKEN       Vercel のアクセストークン（必須、dry-run 時は不要）
  VERCEL_PROJECT_ID  プロジェクト ID。未指定なら .vercel/project.json から取得
  VERCEL_TEAM_ID     チーム(Org) ID。未指定なら .vercel/project.json の orgId

環境変数（GitHub）:
  GITHUB_TOKEN  GitHub のアクセストークン（必須、dry-run 時は不要）
  GITHUB_REPO   owner/repo 形式のリポジトリ名（未指定なら git remote origin から取得）

YAML の kind フィールド（GitHub モード）:
  kind: secret    GitHub Actions Secrets として登録（sealed box 暗号化、値は閲覧不可）
  kind: variable  GitHub Actions Variables として登録（平文）
  ※ 未指定時のデフォルトは secret（安全側）
`)
}

// parseDotenv は .env テキストを key=value のマップに展開する。
func parseDotenv(text string) map[string]string {
	out := map[string]string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSuffix(raw, "\r")
		line = trimExportPrefix(line)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq == -1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		out[key] = value
	}
	return out
}

// trimExportPrefix は行頭の `export ` を取り除く（先頭の空白も許容）。
func trimExportPrefix(line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	const prefix = "export "
	if strings.HasPrefix(trimmed, prefix) {
		// 元の行頭空白は捨てて、export 以降を返す。
		return strings.TrimLeft(trimmed[len(prefix):], " \t")
	}
	return line
}

// normalizeTarget は target 指定（文字列 / 配列）を文字列スライスへ正規化する。
func normalizeTarget(t interface{}) []string {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case string:
		return []string{strings.TrimSpace(v)}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, strings.TrimSpace(fmt.Sprint(x)))
		}
		return out
	case []string:
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, strings.TrimSpace(x))
		}
		return out
	default:
		return []string{strings.TrimSpace(fmt.Sprint(v))}
	}
}

// parseErrorBody は Vercel のエラーレスポンス本文からメッセージを取り出す。
func parseErrorBody(r io.Reader) string {
	data, err := io.ReadAll(r)
	if err != nil {
		return ""
	}
	var body struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return ""
	}
	if body.Error.Message != "" {
		return body.Error.Message
	}
	if body.Error.Code != "" {
		return body.Error.Code
	}
	return "unknown error"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func sortedKeys(m map[string]varConf) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedSet(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ---- init サブコマンド ----

// initOptions は init サブコマンドのフラグ値を保持する。
type initOptions struct {
	env   string
	def   string
	force bool
}

// parseInitFlags は init サブコマンドのコマンドライン引数を解析する。
func parseInitFlags(argv []string) initOptions {
	opts := initOptions{env: ".env", def: "env-sync.yaml"}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		next := func() string {
			i++
			if i >= len(argv) {
				fmt.Fprintf(os.Stderr, "エラー: %s には値が必要です\n", arg)
				os.Exit(1)
			}
			return argv[i]
		}
		// requireValue は空文字のパス指定（例: --env=）を弾く。
		// 空のまま後段に渡すと分かりにくいエラーになるため parse 時点で終了する。
		requireValue := func(flag, v string) string {
			if v == "" {
				fmt.Fprintf(os.Stderr, "エラー: %s には空でない値が必要です\n", flag)
				os.Exit(1)
			}
			return v
		}
		switch {
		case arg == "--env" || arg == "-env":
			opts.env = requireValue("--env", next())
		case strings.HasPrefix(arg, "--env="):
			opts.env = requireValue("--env", strings.TrimPrefix(arg, "--env="))
		case arg == "--def" || arg == "-def":
			opts.def = requireValue("--def", next())
		case strings.HasPrefix(arg, "--def="):
			opts.def = requireValue("--def", strings.TrimPrefix(arg, "--def="))
		case arg == "--force" || arg == "-force":
			opts.force = true
		case arg == "-h" || arg == "--help":
			printUsage()
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "エラー: 不明な引数: %s\n", arg)
			printUsage()
			os.Exit(1)
		}
	}
	return opts
}

// buildInitYAML は keys から env-sync.yaml の雛形テキストを生成する。
// 値は一切含まない。yaml.Marshal は使わず手組みテキスト生成でコメントを差し込む。
func buildInitYAML(keys []string) string {
	var sb strings.Builder

	sb.WriteString("# Vercel に登録する環境変数の type / target 定義。\n")
	sb.WriteString("#\n")
	sb.WriteString("# 値はこのファイルには書かない（git にコミットされるため）。値は .env(.production) から取得する。\n")
	sb.WriteString("# ここに宣言が無いキーは登録されない（.env にあっても警告のうえスキップされる）。\n")
	sb.WriteString("#\n")
	sb.WriteString("#   type:   plain | encrypted | sensitive\n")
	sb.WriteString("#           - encrypted : ダッシュボードで値を再表示できる通常の Variables\n")
	sb.WriteString("#           - sensitive : 保存後は値を読めないシークレット向け\n")
	sb.WriteString("#           - plain     : 暗号化なしの平文\n")
	sb.WriteString("#   target: production | preview | development の配列\n")
	sb.WriteString("#\n")
	sb.WriteString("# !! 以下は init が生成した雛形です。type は投入前に必ず見直すこと !!\n")
	sb.WriteString("# !! NEXT_PUBLIC_ プレフィックスは encrypted、それ以外は sensitive を初期値としています。!!\n")
	sb.WriteString("\n")
	sb.WriteString("defaults:\n")
	sb.WriteString("  target: [production, preview]\n")
	sb.WriteString("  type: sensitive\n")
	sb.WriteString("\n")
	sb.WriteString("variables:\n")

	if len(keys) == 0 {
		sb.WriteString("  # ---- 例 ----\n")
		sb.WriteString("  # NEXT_PUBLIC_API_BASE_URL: { type: encrypted }\n")
		sb.WriteString("  # DATABASE_URL:             { type: sensitive }\n")
		sb.WriteString("  # DEBUG_FLAG:               { type: encrypted, target: [development] }\n")
		return sb.String()
	}

	for _, key := range keys {
		var typ string
		if strings.HasPrefix(key, "NEXT_PUBLIC_") {
			typ = "encrypted"
		} else {
			typ = "sensitive"
		}
		sb.WriteString("  ")
		sb.WriteString(yamlKey(key))
		sb.WriteString(": { type: ")
		sb.WriteString(typ)
		sb.WriteString(" }\n")
	}

	return sb.String()
}

// yamlKey は YAML のマップキーとして安全に出力できる形に整える。
// 環境変数名として一般的な ^[A-Za-z_][A-Za-z0-9_]*$ はそのまま、
// それ以外（空白や : を含む等）は単一引用符でクォートして
// 生成 YAML がパース不能にならないようにする。
func yamlKey(key string) string {
	if isSafeYAMLKey(key) {
		return key
	}
	// 単一引用符内のリテラル ' は '' でエスケープする。
	return "'" + strings.ReplaceAll(key, "'", "''") + "'"
}

// isSafeYAMLKey は key が ^[A-Za-z_][A-Za-z0-9_]*$ に一致するかを返す。
func isSafeYAMLKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 {
			if !isLetter {
				return false
			}
		} else if !isLetter && !isDigit {
			return false
		}
	}
	return true
}

// runInit は init サブコマンドのメイン処理。
func runInit(argv []string) error {
	opts := parseInitFlags(argv)

	// os.ReadFile のエラーで分岐する。fileExists での事前チェックは
	// 権限エラー等を「見つかりません」と誤判定し得るため使わない。
	envText, err := os.ReadFile(opts.env)
	if err != nil {
		if os.IsNotExist(err) {
			return die("env ファイルが見つかりません: %s", opts.env)
		}
		return die("env ファイルの読み込みに失敗: %s: %s", opts.env, err)
	}
	envVars := parseDotenv(string(envText))
	keys := sortedStrKeys(envVars)

	text := buildInitYAML(keys)

	// 上書き保護は O_CREATE|O_EXCL でアトミックに行う。
	// fileExists → WriteFile の2段階では TOCTOU 競合で意図せず
	// 上書きし得るため、open のフラグで排他制御する。
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !opts.force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(opts.def, flags, 0o644)
	if err != nil {
		if !opts.force && os.IsExist(err) {
			return die("既に存在します: %s（上書きするには --force）", opts.def)
		}
		return die("定義ファイルの書き込みに失敗: %s: %s", opts.def, err)
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return die("定義ファイルの書き込みに失敗: %s: %s", opts.def, err)
	}
	if err := f.Close(); err != nil {
		return die("定義ファイルの書き込みに失敗: %s: %s", opts.def, err)
	}

	fmt.Printf("生成しました: %s\n", opts.def)
	fmt.Printf("キー数: %d\n", len(keys))
	if len(keys) > 0 {
		fmt.Printf("キー一覧:\n")
		for _, k := range keys {
			fmt.Printf("  %s\n", k)
		}
	}
	fmt.Println()
	fmt.Println("※ type は投入前に必ず見直してください。値はファイルに書かれていません。")

	return nil
}
