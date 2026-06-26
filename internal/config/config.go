// Package config はコマンドラインフラグ・定義 YAML モデル・dotenv パーサ・汎用ヘルパーを提供する。
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

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
		return fmt.Errorf("provider は文字列または文字列の配列で指定してください")
	}
	return nil
}

// VarConf は定義 YAML の variables 配下 1 件分の設定。
type VarConf struct {
	Secret       *bool        `yaml:"secret"`
	Environments []string     `yaml:"environments"`
	Provider     *ProviderVal `yaml:"provider"`
}

// Definition は定義 YAML 全体の構造。
type Definition struct {
	Defaults struct {
		Secret       *bool        `yaml:"secret"`
		Environments []string     `yaml:"environments"`
		Provider     *ProviderVal `yaml:"provider"`
	} `yaml:"defaults"`
	Variables map[string]VarConf `yaml:"variables"`
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
				fmt.Fprintf(os.Stderr, "エラー: %s には値が必要です\n", arg)
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
		case arg == "--provider" || arg == "-provider":
			v := next()
			if !provider.IsRegisteredProvider(v) {
				names := strings.Join(provider.RegisteredProviderNames(), " または ")
				fmt.Fprintf(os.Stderr, "エラー: --provider には %s を指定してください\n", names)
				os.Exit(1)
			}
			opts.Provider = v
		case strings.HasPrefix(arg, "--provider="):
			v := strings.TrimPrefix(arg, "--provider=")
			if !provider.IsRegisteredProvider(v) {
				names := strings.Join(provider.RegisteredProviderNames(), " または ")
				fmt.Fprintf(os.Stderr, "エラー: --provider には %s を指定してください\n", names)
				os.Exit(1)
			}
			opts.Provider = v
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
			fmt.Fprintf(os.Stderr, "エラー: 不明な引数: %s\n", arg)
			if printUsageFn != nil {
				printUsageFn()
			}
			os.Exit(1)
		}
	}
	return opts
}
