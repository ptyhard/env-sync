// Package config はコマンドラインフラグ・定義 YAML モデル・dotenv パーサ・汎用ヘルパーを提供する。
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// ProviderVal は YAML で string か []string を受け付ける provider フィールドの型。
// `provider: vercel` でも `provider: [vercel, github]` でも解析できる。
type ProviderVal struct {
	Values []string
}

// UnmarshalYAML は ProviderVal の YAML デシリアライズを実装する。
func (p *ProviderVal) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		p.Values = []string{value.Value}
	case yaml.SequenceNode:
		var ss []string
		if err := value.Decode(&ss); err != nil {
			return err
		}
		p.Values = ss
	default:
		return fmt.Errorf("%s", i18n.T(i18n.MsgProviderStringOrArray))
	}
	return nil
}

// VarConf は定義 YAML の variables 配下 1 件分の設定。
type VarConf struct {
	Secret        *bool        `yaml:"secret"`
	Environments  []string     `yaml:"environments"`
	Provider      *ProviderVal `yaml:"provider"`
	VercelProject *ProviderVal `yaml:"vercel_project"`
}

// Definition は定義 YAML 全体の構造。
type Definition struct {
	// Prune が true のとき、variables に定義されていないリモートの変数を削除する。
	// CLI の --prune フラグでも有効化できる（どちらかが true なら prune 実行）。
	Prune bool `yaml:"prune"`
	// PruneExclude は prune で削除しないキー名の glob パターン一覧（例: [BLOB_*, SENTRY_DSN]）。
	// 照合は大文字小文字を区別しない。
	PruneExclude []string `yaml:"prune_exclude"`
	Defaults     struct {
		Secret        *bool        `yaml:"secret"`
		Environments  []string     `yaml:"environments"`
		Provider      *ProviderVal `yaml:"provider"`
		VercelProject *ProviderVal `yaml:"vercel_project"`
	} `yaml:"defaults"`
	Variables map[string]VarConf `yaml:"variables"`
}

// PrescanLang は argv から --lang / --language フラグの値を先読みして返す。
// フラグが見つからない場合は "" を返す。
// プレスキャンは言語決定のための先読みのみを目的とし、他のフラグは無視する。
func PrescanLang(argv []string) string {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--lang" || arg == "-lang" || arg == "--language" || arg == "-language" {
			if i+1 < len(argv) {
				return argv[i+1]
			}
		}
		if strings.HasPrefix(arg, "--lang=") {
			return strings.TrimPrefix(arg, "--lang=")
		}
		if strings.HasPrefix(arg, "--language=") {
			return strings.TrimPrefix(arg, "--language=")
		}
	}
	return ""
}

// ParseFlags はコマンドライン引数を解析して Options を返す。
// flag パッケージは特殊な短縮形 (-y) と長形 (--yes) の両立や --dry-run の扱いが
// 煩雑なため手で処理する。
// --help / 不明な引数は printUsageFn を呼んで os.Exit する。
// --version は引数の出現順に処理され versionFn を呼んで os.Exit(0) する。
func ParseFlags(argv []string, printUsageFn func(), versionFn func()) provider.Options {
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml", Provider: "vercel"}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		next := func() string {
			i++
			if i >= len(argv) {
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgFlagNeedsValue, arg))
				os.Exit(1)
			}
			return argv[i]
		}
		switch {
		case arg == "--env" || arg == "-env":
			opts.Env = next()
		case strings.HasPrefix(arg, "--env="):
			opts.Env = strings.TrimPrefix(arg, "--env=")
		case arg == "--def" || arg == "-def":
			opts.Def = next()
		case strings.HasPrefix(arg, "--def="):
			opts.Def = strings.TrimPrefix(arg, "--def=")
		case arg == "--dry-run" || arg == "-dry-run":
			opts.DryRun = true
		case arg == "--yes" || arg == "-yes" || arg == "-y":
			opts.Yes = true
		case arg == "--prune" || arg == "-prune":
			opts.Prune = true
		case arg == "--provider" || arg == "-provider":
			v := next()
			if !provider.IsRegisteredProvider(v) {
				names := strings.Join(provider.RegisteredProviderNames(), i18n.T(i18n.MsgFlagOrSeparator))
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgFlagProviderInvalid, names))
				os.Exit(1)
			}
			opts.Provider = v
		case strings.HasPrefix(arg, "--provider="):
			v := strings.TrimPrefix(arg, "--provider=")
			if !provider.IsRegisteredProvider(v) {
				names := strings.Join(provider.RegisteredProviderNames(), i18n.T(i18n.MsgFlagOrSeparator))
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgFlagProviderInvalid, names))
				os.Exit(1)
			}
			opts.Provider = v
		case arg == "--environments" || arg == "-environments":
			v := splitEnvironments(next())
			if len(v) == 0 {
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgFlagNeedsNonEmpty, "--environments"))
				os.Exit(1)
			}
			opts.Environments = v
		case strings.HasPrefix(arg, "--environments="):
			v := splitEnvironments(strings.TrimPrefix(arg, "--environments="))
			if len(v) == 0 {
				fmt.Fprint(os.Stderr, i18n.T(i18n.MsgFlagNeedsNonEmpty, "--environments"))
				os.Exit(1)
			}
			opts.Environments = v
		case arg == "--vercel-project" || arg == "-vercel-project":
			opts.VercelProject = next()
		case strings.HasPrefix(arg, "--vercel-project="):
			opts.VercelProject = strings.TrimPrefix(arg, "--vercel-project=")
		case arg == "--github-repo" || arg == "-github-repo":
			opts.GitHubRepo = next()
		case strings.HasPrefix(arg, "--github-repo="):
			opts.GitHubRepo = strings.TrimPrefix(arg, "--github-repo=")
		case arg == "--lang" || arg == "-lang" || arg == "--language" || arg == "-language":
			opts.Language = next()
		case strings.HasPrefix(arg, "--lang="):
			opts.Language = strings.TrimPrefix(arg, "--lang=")
		case strings.HasPrefix(arg, "--language="):
			opts.Language = strings.TrimPrefix(arg, "--language=")
		case arg == "--version" || arg == "-version":
			if versionFn != nil {
				versionFn()
			}
			os.Exit(0)
		case arg == "-h" || arg == "--help":
			if printUsageFn != nil {
				printUsageFn()
			}
			os.Exit(0)
		default:
			fmt.Fprint(os.Stderr, i18n.T(i18n.MsgFlagUnknown, arg))
			if printUsageFn != nil {
				printUsageFn()
			}
			os.Exit(1)
		}
	}
	return opts
}

// splitEnvironments はカンマ区切りの環境名文字列を分割・トリム・空要素除去して返す。
// --environments フラグの値の解析に使用する。
func splitEnvironments(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}
