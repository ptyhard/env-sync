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
	"github.com/ptyhard/env-sync/internal/i18n"
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
		fmt.Fprint(os.Stderr, i18n.T(i18n.MsgErrorPrefix, err))
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]

	if len(args) > 0 && args[0] == "init" {
		// --lang フラグと AppConfig の language フィールドも言語解決に含める。
		// PrescanLang はフラグ解析前に --lang/--language の値だけを先読みする。
		prescannedLang := config.PrescanLang(args[1:])
		// config 読み込み失敗時は configVal を "" として継続する。
		// エラーは後続の本処理（init ロジック内の LoadAppConfig 呼び出し等）で顕在化する。
		var configLang string
		if appCfg, err := config.LoadAppConfig(); err == nil {
			configLang = appCfg.Language
		}
		i18n.SetLang(string(i18n.Resolve(prescannedLang, os.Getenv("ENV_SYNC_LANG"), configLang)))
		return config.RunInit(args[1:], printUsage)
	}

	if len(args) > 0 && args[0] == "setup" {
		// --lang フラグと AppConfig の language フィールドも言語解決に含める。
		// PrescanLang はフラグ解析前に --lang/--language の値だけを先読みする。
		prescannedLang := config.PrescanLang(args[1:])
		// config 読み込み失敗時は configVal を "" として継続する。
		// エラーは後続の本処理（setup ロジック内の LoadAppConfig 呼び出し等）で顕在化する。
		var configLang string
		if appCfg, err := config.LoadAppConfig(); err == nil {
			configLang = appCfg.Language
		}
		i18n.SetLang(string(i18n.Resolve(prescannedLang, os.Getenv("ENV_SYNC_LANG"), configLang)))
		return config.RunSetup(args[1:], printUsage)
	}

	if len(args) > 0 && args[0] == "validate" {
		// --lang フラグと AppConfig の language フィールドも言語解決に含める。
		// PrescanLang はフラグ解析前に --lang/--language の値だけを先読みする。
		prescannedLang := config.PrescanLang(args[1:])
		// config 読み込み失敗時は configLang を "" として継続する。
		// エラーは後続の本処理（validate ロジック内の LoadAppConfig 呼び出し等）で顕在化する。
		var configLang string
		if appCfg, err := config.LoadAppConfig(); err == nil {
			configLang = appCfg.Language
		}
		i18n.SetLang(string(i18n.Resolve(prescannedLang, os.Getenv("ENV_SYNC_LANG"), configLang)))
		return runValidate(args[1:], printUsage)
	}

	printVersion := func() {
		v, c, d := versionInfo()
		fmt.Printf("env-sync version %s (commit: %s, built: %s)\n", v, c, d)
	}

	// ParseFlags の内部で --help/--version が処理されて os.Exit(0) するため、
	// その前に言語を確定させる必要がある。init/setup と同じパターンで先読みする。
	// PrescanLang はメイン経路ではサブコマンド名が無いため args をそのまま渡す。
	prescannedLang := config.PrescanLang(args)
	// config 読み込み失敗時は configLang を "" として継続する。
	// エラーは後続の本処理（provider.Sync 内の LoadAppConfig 呼び出し）で顕在化する。
	var configLang string
	if appCfg, err := config.LoadAppConfig(); err == nil {
		configLang = appCfg.Language
	}
	i18n.SetLang(string(i18n.Resolve(prescannedLang, os.Getenv("ENV_SYNC_LANG"), configLang)))

	opts := config.ParseFlags(args, printUsage, printVersion)

	// ParseFlags 後に opts.Language（正式解析済み）で言語を再確定する。
	// configLang は上で取得済みのため LoadAppConfig を再呼び出ししない。
	i18n.SetLang(string(i18n.Resolve(opts.Language, os.Getenv("ENV_SYNC_LANG"), configLang)))

	// ---- 入力読み込み ----
	if !config.FileExists(opts.Env) {
		return fmt.Errorf("%s", i18n.T(i18n.MsgEnvFileNotFound, opts.Env))
	}
	if !config.FileExists(opts.Def) {
		return fmt.Errorf("%s", i18n.T(i18n.MsgDefFileNotFound, opts.Def))
	}

	envText, err := os.ReadFile(opts.Env)
	if err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.MsgEnvFileReadFail, err))
	}
	envVars := config.ParseDotenv(string(envText))

	defText, err := os.ReadFile(opts.Def)
	if err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.MsgDefFileReadFail, err))
	}
	var def config.Definition
	if err := yaml.Unmarshal(defText, &def); err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.MsgDefFileYAMLFail, err))
	}

	// ---- 整合性チェック（provider 共通） ----
	defKeys := config.SortedKeys(def.Variables)
	defKeySet := make(map[string]bool, len(defKeys))
	for _, k := range defKeys {
		defKeySet[k] = true
	}
	for _, k := range defKeys {
		if _, ok := envVars[k]; !ok {
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgSkipNoValueInEnv, k, opts.Env))
		}
	}
	for _, k := range config.SortedStrKeys(envVars) {
		if !defKeySet[k] {
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgSkipNotDefined, k, opts.Env))
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
		fmt.Println(i18n.T(i18n.MsgNoEntries))
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
	fmt.Fprint(os.Stderr, i18n.T(i18n.MsgUsage))
}
