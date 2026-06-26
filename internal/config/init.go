package config

import (
	"fmt"
	"os"
	"strings"
)

// InitOptions は init サブコマンドのフラグ値を保持する。
type InitOptions struct {
	Env   string
	Def   string
	Force bool
}

// ParseInitFlags は init サブコマンドのコマンドライン引数を解析する。
func ParseInitFlags(argv []string, printUsageFn func()) InitOptions {
	opts := InitOptions{Env: ".env", Def: "env-sync.yaml"}
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
		requireValue := func(flag, v string) string {
			if v == "" {
				fmt.Fprintf(os.Stderr, "エラー: %s には空でない値が必要です\n", flag)
				os.Exit(1)
			}
			return v
		}
		switch {
		case arg == "--env" || arg == "-env":
			opts.Env = requireValue("--env", next())
		case strings.HasPrefix(arg, "--env="):
			opts.Env = requireValue("--env", strings.TrimPrefix(arg, "--env="))
		case arg == "--def" || arg == "-def":
			opts.Def = requireValue("--def", next())
		case strings.HasPrefix(arg, "--def="):
			opts.Def = requireValue("--def", strings.TrimPrefix(arg, "--def="))
		case arg == "--force" || arg == "-force":
			opts.Force = true
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

// BuildInitYAML は keys から env-sync.yaml の雛形テキストを生成する。
// 値は一切含まない。yaml.Marshal は使わず手組みテキスト生成でコメントを差し込む。
func BuildInitYAML(keys []string) string {
	var sb strings.Builder

	sb.WriteString("# Vercel / GitHub Actions に登録する環境変数の定義。\n")
	sb.WriteString("#\n")
	sb.WriteString("# 値はこのファイルには書かない（git にコミットされるため）。値は .env(.production) から取得する。\n")
	sb.WriteString("# ここに宣言が無いキーは登録されない（.env にあっても警告のうえスキップされる）。\n")
	sb.WriteString("#\n")
	sb.WriteString("#   secret: true|false\n")
	sb.WriteString("#           - true  : シークレットとして登録（Vercel: sensitive / GitHub: Secret）\n")
	sb.WriteString("#           - false : 平文として登録（Vercel: plain / GitHub: Variable）\n")
	sb.WriteString("#   environments: []  登録先環境の配列\n")
	sb.WriteString("#           Vercel: production|preview|development（空なら production,preview）\n")
	sb.WriteString("#           GitHub: named environment 名（空なら repo レベル）\n")
	sb.WriteString("#\n")
	sb.WriteString("# !! 以下は init が生成した雛形です。secret は投入前に必ず見直すこと !!\n")
	sb.WriteString("# !! NEXT_PUBLIC_ プレフィックスは secret: false、それ以外は secret: true を初期値としています。!!\n")
	sb.WriteString("\n")
	sb.WriteString("defaults:\n")
	sb.WriteString("  secret: true\n")
	sb.WriteString("\n")
	sb.WriteString("variables:\n")

	if len(keys) == 0 {
		sb.WriteString("  # ---- 例 ----\n")
		sb.WriteString("  # NEXT_PUBLIC_API_BASE_URL: { secret: false }\n")
		sb.WriteString("  # DATABASE_URL:             { secret: true }\n")
		sb.WriteString("  # STAGING_KEY:              { secret: true, environments: [production] }\n")
		return sb.String()
	}

	for _, key := range keys {
		var secret string
		if strings.HasPrefix(key, "NEXT_PUBLIC_") {
			secret = "false"
		} else {
			secret = "true"
		}
		sb.WriteString("  ")
		sb.WriteString(yamlKey(key))
		sb.WriteString(": { secret: ")
		sb.WriteString(secret)
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

// RunInit は init サブコマンドのメイン処理。
func RunInit(argv []string, printUsageFn func()) error {
	opts := ParseInitFlags(argv, printUsageFn)

	// os.ReadFile のエラーで分岐する。FileExists での事前チェックは
	// 権限エラー等を「見つかりません」と誤判定し得るため使わない。
	envText, err := os.ReadFile(opts.Env)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("env ファイルが見つかりません: %s", opts.Env)
		}
		return fmt.Errorf("env ファイルの読み込みに失敗: %s: %s", opts.Env, err)
	}
	envVars := ParseDotenv(string(envText))
	keys := SortedStrKeys(envVars)

	text := BuildInitYAML(keys)

	// 上書き保護は O_CREATE|O_EXCL でアトミックに行う。
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !opts.Force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(opts.Def, flags, 0o644)
	if err != nil {
		if !opts.Force && os.IsExist(err) {
			return fmt.Errorf("既に存在します: %s（上書きするには --force）", opts.Def)
		}
		return fmt.Errorf("定義ファイルの書き込みに失敗: %s: %s", opts.Def, err)
	}
	if _, err := f.WriteString(text); err != nil {
		f.Close()
		return fmt.Errorf("定義ファイルの書き込みに失敗: %s: %s", opts.Def, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("定義ファイルの書き込みに失敗: %s: %s", opts.Def, err)
	}

	fmt.Printf("生成しました: %s\n", opts.Def)
	fmt.Printf("キー数: %d\n", len(keys))
	if len(keys) > 0 {
		fmt.Printf("キー一覧:\n")
		for _, k := range keys {
			fmt.Printf("  %s\n", k)
		}
	}
	fmt.Println()
	fmt.Println("※ secret は投入前に必ず見直してください。値はファイルに書かれていません。")

	return nil
}
