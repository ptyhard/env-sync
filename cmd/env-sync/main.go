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
//	VERCEL_PROJECT_ID   プロジェクト ID。未指定なら config ファイルまたは .vercel/project.json から取得
//
// 任意 (Vercel):
//
//	VERCEL_TEAM_ID      チーム(Org) ID。未指定なら config ファイルまたは .vercel/project.json の orgId
//
// 必須 (GitHub):
//
//	GITHUB_TOKEN        GitHub のアクセストークン（dry-run 時は不要）
//	GITHUB_REPO         owner/repo 形式。未指定なら config ファイルまたは git remote から取得
//
// config ファイル:
//
// 環境変数の代わりに YAML ファイルでトークン・ID を設定できる。
// 解決優先順位: 環境変数 > project config > global config > 既存フォールバック
//
//	global:  ~/.config/env-sync/config.yaml (XDG_CONFIG_HOME を尊重)
//	project: .env-sync.config.yaml (カレントディレクトリ)
//
// YAML スキーマ（単一プロジェクト）:
//
//	vercel:
//	  token:      <Vercel トークン>
//	  project_id: <プロジェクト ID>
//	  team_id:    <チーム ID>
//	github:
//	  token: <GitHub トークン>
//	  repo:  <owner/repo>
//
// YAML スキーマ（モノレポ: 複数プロジェクト / 複数リポジトリ）:
//
//	vercel:
//	  token: <デフォルトトークン>
//	  team_id: <デフォルトチーム ID>
//	  projects:
//	    - name: app-a
//	      project_id: <プロジェクト ID>
//	    - name: app-b
//	      project_id: <プロジェクト ID>
//	      token: <per-project トークン（任意）>
//	github:
//	  token: <デフォルトトークン>
//	  repos:
//	    - name: frontend
//	      repo: org/frontend
//	    - name: backend
//	      repo: org/backend
//	      token: <per-repo トークン（任意）>
//
// セキュリティ: global config にトークンが含まれていてパーミッションが 0600 でない場合は警告を出力する。
//
// 必須 (GCP):
//
//	GCP_PROJECT_ID      Secret Manager の対象 GCP プロジェクト ID
//	認証: Application Default Credentials（ADC）を使用。gcloud auth application-default login 等で設定する。
//
// オプション:
//
//	--provider <name>         同期先（デフォルト vercel）
//	--env  <file>             値を読む env ファイル（デフォルト .env）
//	--def  <file>             定義 YAML（デフォルト env-sync.yaml）
//	--dry-run                 実際には送信せず、新規/更新の区別を含む登録予定一覧を表示（値は出さない）
//	--yes, -y                 更新(上書き)を含む場合の確認をスキップして送信
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"gopkg.in/yaml.v3"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/provider"
	internalsync "github.com/ptyhard/env-sync/internal/sync"

	_ "github.com/ptyhard/env-sync/internal/provider/gcp"
	_ "github.com/ptyhard/env-sync/internal/provider/github"
	_ "github.com/ptyhard/env-sync/internal/provider/vercel"
)

// ldflags で注入するバージョン情報。
// goreleaser のリリースビルドでは ldflags で上書きされる。
// ローカルの go build / go install では初期値のままになるため、
// versionInfo() が runtime/debug のビルド情報をフォールバックとして補う。
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionInfo は表示用のバージョン・コミット・日時を返す。
// 返す日時は ldflags の date（ビルド日時）、またはフォールバック時は
// runtime/debug の vcs.time（コミット時刻）であり、厳密にはビルド日時とは異なる場合がある。
// ldflags で値が注入されていればそれを優先し、無い場合は
// go が埋め込む VCS 情報（go build）やモジュールバージョン（go install module@v）で補う。
// go build でモジュールバージョンが "(devel)" のときは vcs.revision の先頭 7 文字を使って
// "dev-<shortsha>" 形式にフォールバックする。
// version のみ注入されて commit/date が未注入のケースでも ReadBuildInfo() で補えるよう、
// 3 つすべてが初期値でない場合のみ早期リターンする。
func versionInfo() (v, c, d string) {
	v, c, d = version, commit, date
	if v != "dev" && c != "none" && d != "unknown" {
		// ldflags で 3 つとも注入済み。フォールバック不要。
		return v, c, d
	}

	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return v, c, d
	}

	// go install module@v1.2.3 ではモジュールバージョンが入る（go build では "(devel)"）。
	// v が "dev"（未注入）の場合のみ上書きし、ldflags 注入済みの値は保持する。
	if v == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		v = bi.Main.Version
	}

	var revision string
	var modified bool
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
			if c == "none" {
				c = s.Value
			}
		case "vcs.time":
			if d == "unknown" {
				d = s.Value
			}
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if modified && c != "none" {
		c += "-dirty"
	}
	// go build では bi.Main.Version が "(devel)" のため v が "dev" のままになる。
	// vcs.revision が得られた場合は "dev-<shortsha>" 形式でバージョンを補う。
	if v == "dev" && revision != "" {
		short := revision
		if len(short) > 7 {
			short = short[:7]
		}
		v = "dev-" + short
		if modified {
			v += "-dirty"
		}
	}
	return v, c, d
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
		return config.RunInit(args[1:], printUsage)
	}

	if len(args) > 0 && args[0] == "setup" {
		return config.RunSetup(args[1:], printUsage)
	}

	printVersion := func() {
		v, c, d := versionInfo()
		fmt.Printf("env-sync version %s (commit: %s, built: %s)\n", v, c, d)
	}
	opts := config.ParseFlags(args, printUsage, printVersion)

	// ---- 入力読み込み ----
	if !config.FileExists(opts.Env) {
		return die("env ファイルが見つかりません: %s", opts.Env)
	}
	if !config.FileExists(opts.Def) {
		return die("定義ファイルが見つかりません: %s", opts.Def)
	}

	envText, err := os.ReadFile(opts.Env)
	if err != nil {
		return die("env ファイルの読み込みに失敗: %s", err)
	}
	envVars := config.ParseDotenv(string(envText))

	defText, err := os.ReadFile(opts.Def)
	if err != nil {
		return die("定義ファイルの読み込みに失敗: %s", err)
	}
	var def config.Definition
	if err := yaml.Unmarshal(defText, &def); err != nil {
		return die("定義ファイルの YAML パースに失敗: %s", err)
	}

	// ---- 整合性チェック（provider 共通） ----
	defKeys := config.SortedKeys(def.Variables)
	defKeySet := make(map[string]bool, len(defKeys))
	for _, k := range defKeys {
		defKeySet[k] = true
	}
	for _, k := range defKeys {
		if _, ok := envVars[k]; !ok {
			fmt.Fprintf(os.Stderr, "⚠ %s: 定義にあるが %s に値が無いためスキップ\n", k, opts.Env)
		}
	}
	for _, k := range config.SortedStrKeys(envVars) {
		if !defKeySet[k] {
			fmt.Fprintf(os.Stderr, "⚠ %s: %s にあるが定義に無いためスキップ\n", k, opts.Env)
		}
	}

	// ---- Entry に変換（provider 解決を含む） ----
	entries, err := internalsync.ResolveEntries(def, envVars, defKeys, opts.Provider)
	if err != nil {
		return err
	}

	// ---- プロバイダーごとに振り分け ----
	providerEntries := map[string][]provider.Entry{}
	for _, e := range entries {
		for _, pname := range e.Providers {
			providerEntries[pname] = append(providerEntries[pname], e)
		}
	}

	// entries が 0 件なら同期対象なしを明示して終了
	if len(entries) == 0 {
		fmt.Println("登録対象がありません")
		return nil
	}

	// ---- プロバイダーへ同期（登録順） ----
	// dry-run 時も各 provider が新規/更新を分類して一覧表示した上で終了する
	for _, pname := range provider.RegisteredProviderNames() {
		ents, ok := providerEntries[pname]
		if !ok {
			continue
		}
		p, _ := provider.LookupProvider(pname)
		if err := p.Sync(opts, ents); err != nil {
			return err
		}
	}
	return nil
}

func printUsage() {
	fmt.Fprint(os.Stderr, `env-sync - 定義ファイルで宣言した環境変数を Vercel または GitHub Actions へ一括登録(同期)する

サブコマンド:
  init    .env から env-sync.yaml の雛形を生成する
  setup   認証情報 config ファイル（.env-sync.config.yaml / ~/.config/env-sync/config.yaml）を対話生成する

使い方:
  VERCEL_TOKEN=xxxxx env-sync [オプション]
  GITHUB_TOKEN=xxxxx env-sync --provider github [オプション]
  env-sync init [--env <file>] [--def <file>] [--force]
  env-sync setup [--global] [--force]

オプション（同期）:
  --provider <name>         同期先（デフォルト vercel）
  --env <file>              値を読む env ファイル（デフォルト .env）
  --def <file>              定義 YAML（デフォルト env-sync.yaml）
  --dry-run                 送信せず新規/更新の区別を含む登録予定一覧を表示（値は出さない）
  --yes, -y                 更新(上書き)を含む場合の確認をスキップして送信
  --vercel-project <name>   config の vercel.projects から指定名のプロジェクトのみ同期（モノレポ対応）
  --github-repo <name>      config の github.repos から指定名のリポジトリのみ同期（モノレポ対応）
  --version                 バージョン情報を表示して終了
  -h, --help                このヘルプを表示

確認動作:
  送信前に provider へ問い合わせ、既存 key を「⟳ KEY [更新]」、未登録 key を「+ KEY [新規]」として表示する。
  更新(上書き)対象がある場合のみ確認プロンプトが出る。新規のみなら確認なしで送信する。
  非対話環境(TTY なし)で更新対象があり --yes/-y がない場合はエラーで停止し --yes を案内する。

オプション（init）:
  --env <file>   読み込む env ファイル（デフォルト .env）
  --def <file>   出力する YAML ファイル（デフォルト env-sync.yaml）
  --force        既存の def ファイルを上書きする

オプション（setup）:
  --global       ~/.config/env-sync/config.yaml（XDG_CONFIG_HOME 尊重）へ出力（デフォルトは .env-sync.config.yaml）
  --force        既存の config ファイルを上書きする

環境変数（Vercel）:
  VERCEL_TOKEN       Vercel のアクセストークン（必須、dry-run 時は不要）
  VERCEL_PROJECT_ID  プロジェクト ID。未指定なら config ファイルまたは .vercel/project.json から取得
  VERCEL_TEAM_ID     チーム(Org) ID。未指定なら config ファイルまたは .vercel/project.json の orgId

環境変数（GitHub）:
  GITHUB_TOKEN  GitHub のアクセストークン（必須、dry-run 時は不要）
  GITHUB_REPO   owner/repo 形式のリポジトリ名（未指定なら config ファイルまたは git remote origin から取得）

環境変数（GCP）:
  GCP_PROJECT_ID  Secret Manager の対象 GCP プロジェクト ID（必須）
  認証: Application Default Credentials（ADC）を使用。
        GOOGLE_APPLICATION_CREDENTIALS でサービスアカウント鍵を指定、
        または gcloud auth application-default login で ADC を設定する。

config ファイル（環境変数の代替）:
  解決優先順位: 環境変数 > project config > global config > 既存フォールバック
  global:  ~/.config/env-sync/config.yaml  (XDG_CONFIG_HOME を尊重)
  project: .env-sync.config.yaml           (カレントディレクトリ)

  スキーマ（単一プロジェクト）:
    vercel:
      token:      <Vercel トークン>
      project_id: <プロジェクト ID>
      team_id:    <チーム ID>
    github:
      token: <GitHub トークン>
      repo:  <owner/repo>

  スキーマ（モノレポ: 複数プロジェクト / 複数リポジトリ）:
    vercel:
      token: <デフォルトトークン（per-project で上書き可）>
      team_id: <デフォルトチーム ID>
      projects:
        - name: app-a
          project_id: <プロジェクト ID>
        - name: app-b
          project_id: <プロジェクト ID>
          token: <per-project トークン（任意）>
          team_id: <per-project チーム ID（任意）>
    github:
      token: <デフォルトトークン（per-repo で上書き可）>
      repos:
        - name: frontend
          repo: org/frontend
        - name: backend
          repo: org/backend
          token: <per-repo トークン（任意）>

  ※ global config にトークンが含まれていてパーミッションが 0600 でない場合は警告を出力します

YAML スキーマ（定義ファイル env-sync.yaml）:
  secret: true|false  シークレットとして登録するか（デフォルト true）
                      Vercel: true→sensitive / false→plain
                      GitHub: true→Secret / false→Variable
  environments: []    登録先環境の配列
                      Vercel: production|preview|development（空なら production,preview）
                      GitHub: named environment 名（空なら repo レベル）
                      ※ GitHub の named environment は事前に作成が必要
`)
}
