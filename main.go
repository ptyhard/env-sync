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
//	--provider <name>         同期先（デフォルト vercel）
//	--env  <file>             値を読む env ファイル（デフォルト .env）
//	--def  <file>             定義 YAML（デフォルト env-sync.yaml）
//	--dry-run                 実際には送信せず、登録予定の key/secret/environments だけ表示（値は出さない）
//	--yes, -y                 送信前の確認をスキップ
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ldflags で注入するバージョン情報。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

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

	// ---- Entry に変換 ----
	entries, err := resolveEntries(def, envVars, defKeys)
	if err != nil {
		return err
	}

	// ---- provider で分岐（registry ベース） ----
	p, ok := lookupProvider(opts.provider)
	if !ok {
		return die("未登録の provider: %s", opts.provider)
	}
	return p.Sync(opts, entries)
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
  --provider <name>         同期先（デフォルト vercel）
  --env <file>              値を読む env ファイル（デフォルト .env）
  --def <file>              定義 YAML（デフォルト env-sync.yaml）
  --dry-run                 送信せず登録予定の key/secret/environments だけ表示
  --yes, -y                 送信前の確認をスキップ
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

YAML スキーマ:
  secret: true|false  シークレットとして登録するか（デフォルト true）
                      Vercel: true→sensitive / false→plain
                      GitHub: true→Secret / false→Variable
  environments: []    登録先環境の配列
                      Vercel: production|preview|development（空なら production,preview）
                      GitHub: named environment 名（空なら repo レベル）
                      ※ GitHub の named environment は事前に作成が必要
`)
}
