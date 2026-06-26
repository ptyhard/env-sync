// Command vercel-env-sync は、定義ファイル(vercel-env.yaml)で宣言した環境変数を
// Vercel へ一括登録(同期)する。
//
// 値は定義ファイルには書かず .env(.production) から取得する。
// REST API (POST /v10/projects/{id}/env?upsert=true) を使うため再実行で更新も可能。
//
// 使い方:
//
//	VERCEL_TOKEN=xxxxx vercel-env-sync --env .env.production
//
// 必須:
//
//	VERCEL_TOKEN        Vercel のアクセストークン (https://vercel.com/account/tokens)
//	VERCEL_PROJECT_ID   プロジェクト ID。未指定なら .vercel/project.json から自動取得
//
// 任意:
//
//	VERCEL_TEAM_ID      チーム(Org) ID。未指定なら .vercel/project.json の orgId
//
// オプション:
//
//	--env  <file>   値を読む env ファイル（デフォルト .env）
//	--def  <file>   type/target 定義 YAML（デフォルト vercel-env.yaml）
//	--dry-run       実際には送信せず、登録予定の key/type/target だけ表示（値は出さない）
//	--yes, -y       送信前の確認をスキップ
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

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

// options はコマンドラインフラグの値を保持する。
type options struct {
	env    string
	def    string
	dryRun bool
	yes    bool
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
}

// definition は定義 YAML 全体の構造。
type definition struct {
	Defaults struct {
		Type   string      `yaml:"type"`
		Target interface{} `yaml:"target"`
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

	// ---- 整合性チェック ----
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

// parseFlags はコマンドライン引数を解析する。flag パッケージは特殊な
// 短縮形 (-y) と長形 (--yes) の両立や --dry-run の扱いが煩雑なため手で処理する。
func parseFlags(argv []string) options {
	opts := options{env: ".env", def: "vercel-env.yaml"}
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
			fmt.Printf("vercel-env-sync version %s (commit: %s, built: %s)\n", version, commit, date)
			os.Exit(0)
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
	fmt.Fprint(os.Stderr, `vercel-env-sync - 定義ファイルで宣言した環境変数を Vercel へ一括登録(同期)する

サブコマンド:
  init   .env から vercel-env.yaml の雛形を生成する

使い方:
  VERCEL_TOKEN=xxxxx vercel-env-sync [オプション]
  vercel-env-sync init [--env <file>] [--def <file>] [--force]

オプション（同期）:
  --env <file>   値を読む env ファイル（デフォルト .env）
  --def <file>   type/target 定義 YAML（デフォルト vercel-env.yaml）
  --dry-run      送信せず登録予定の key/type/target だけ表示
  --yes, -y      送信前の確認をスキップ
  --version      バージョン情報を表示して終了
  -h, --help     このヘルプを表示

オプション（init）:
  --env <file>   読み込む env ファイル（デフォルト .env）
  --def <file>   出力する YAML ファイル（デフォルト vercel-env.yaml）
  --force        既存の def ファイルを上書きする

環境変数:
  VERCEL_TOKEN       Vercel のアクセストークン（必須、dry-run 時は不要）
  VERCEL_PROJECT_ID  プロジェクト ID。未指定なら .vercel/project.json から取得
  VERCEL_TEAM_ID     チーム(Org) ID。未指定なら .vercel/project.json の orgId
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
	opts := initOptions{env: ".env", def: "vercel-env.yaml"}
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

// buildInitYAML は keys から vercel-env.yaml の雛形テキストを生成する。
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
