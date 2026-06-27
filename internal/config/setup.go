package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

// SetupOptions は setup サブコマンドのフラグ値を保持する。
type SetupOptions struct {
	Global bool
	Force  bool
}

// SetupAnswers は対話プロンプトで収集した回答を保持する。
type SetupAnswers struct {
	UseVercel       bool
	VercelProjectID string
	VercelTeamID    string
	VercelTokenRef  string // "${VERCEL_TOKEN}" または平文トークン

	UseGitHub      bool
	GitHubRepo     string
	GitHubTokenRef string // "${GITHUB_TOKEN}" または平文トークン

	// 平文トークンが 1 つ以上含まれる場合 true（0600 パーミッション決定に使用）
	HasPlainToken bool
}

// ParseSetupFlags は setup サブコマンドのコマンドライン引数を解析する。
func ParseSetupFlags(argv []string, printUsageFn func()) SetupOptions {
	opts := SetupOptions{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		switch {
		case arg == "--global" || arg == "-global":
			opts.Global = true
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

// BuildSetupYAML は SetupAnswers から認証情報 config の YAML テキストを生成する。
// 生成される YAML は AppConfig スキーマに準拠し LoadAppConfig() でパースできる。
func BuildSetupYAML(answers SetupAnswers) string {
	var sb strings.Builder
	sb.WriteString("# env-sync 認証情報 config\n")
	sb.WriteString("# このファイルをコミットしないよう .gitignore に追記することを推奨します。\n")
	sb.WriteString("# projects[] / repos[]（モノレポ向け複数ターゲット）は手で追記してください。\n")

	if answers.UseVercel {
		sb.WriteString("vercel:\n")
		sb.WriteString("  token: ")
		sb.WriteString(answers.VercelTokenRef)
		sb.WriteString("\n")
		if answers.VercelProjectID != "" {
			sb.WriteString("  project_id: ")
			sb.WriteString(answers.VercelProjectID)
			sb.WriteString("\n")
		}
		if answers.VercelTeamID != "" {
			sb.WriteString("  team_id: ")
			sb.WriteString(answers.VercelTeamID)
			sb.WriteString("\n")
		}
	}

	if answers.UseGitHub {
		sb.WriteString("github:\n")
		sb.WriteString("  token: ")
		sb.WriteString(answers.GitHubTokenRef)
		sb.WriteString("\n")
		if answers.GitHubRepo != "" {
			sb.WriteString("  repo: ")
			sb.WriteString(answers.GitHubRepo)
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// WriteSetupFile は path へ content を書き込む。
// force なしで既存ファイルがある場合はエラーを返す。
func WriteSetupFile(path, content string, perm os.FileMode, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, perm)
	if err != nil {
		if !force && os.IsExist(err) {
			return fmt.Errorf("既に存在します: %s（上書きするには --force）", path)
		}
		return fmt.Errorf("config ファイルの書き込みに失敗: %s: %s", path, err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return fmt.Errorf("config ファイルの書き込みに失敗: %s: %s", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("config ファイルの書き込みに失敗: %s: %s", path, err)
	}
	return nil
}

// RunSetup は setup サブコマンドのメイン処理。非 TTY 環境ではエラーで停止する。
func RunSetup(argv []string, printUsageFn func()) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf(
			"非対話環境（TTY なし）では setup は実行できません。\n" +
				"config ファイルを手で作成するか、次の例を参考にしてください:\n" +
				"  mkdir -p ~/.config/env-sync\n" +
				"  cat > ~/.config/env-sync/config.yaml <<'EOF'\n" +
				"  vercel:\n" +
				"    token: ${VERCEL_TOKEN}\n" +
				"    project_id: <プロジェクト ID>\n" +
				"  github:\n" +
				"    token: ${GITHUB_TOKEN}\n" +
				"    repo: <owner/repo>\n" +
				"  EOF\n" +
				"  chmod 0600 ~/.config/env-sync/config.yaml")
	}
	return RunSetupWithReader(argv, printUsageFn, os.Stdin)
}

// RunSetupWithReader は TTY チェックなしの setup 実装。in からユーザー入力を読む（テスト用）。
func RunSetupWithReader(argv []string, printUsageFn func(), in io.Reader) error {
	opts := ParseSetupFlags(argv, printUsageFn)
	reader := bufio.NewReader(in)

	answers, err := promptSetupAnswers(reader)
	if err != nil {
		return err
	}

	text := BuildSetupYAML(answers)

	var outputPath string
	if opts.Global {
		outputPath = globalAppConfigPath()
		if outputPath == "" {
			return fmt.Errorf("ホームディレクトリが取得できません")
		}
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("ディレクトリの作成に失敗: %s: %s", dir, err)
		}
	} else {
		outputPath = projectAppConfigFile
	}

	// 生 token または --global 出力時は 0600 で作成する
	perm := os.FileMode(0o644)
	if answers.HasPlainToken || opts.Global {
		perm = 0o600
	}

	if err := WriteSetupFile(outputPath, text, perm, opts.Force); err != nil {
		return err
	}

	fmt.Printf("生成しました: %s\n", outputPath)
	if answers.HasPlainToken || opts.Global {
		fmt.Println("トークンが含まれます。パーミッションは 0600 です。")
	}
	fmt.Println()
	fmt.Println("※ このファイルを .gitignore に追記することを推奨します。")

	return nil
}

func promptSetupAnswers(reader *bufio.Reader) (SetupAnswers, error) {
	var answers SetupAnswers

	// Vercel
	fmt.Print("Vercel を設定しますか？ (Y/n): ")
	useVercel, err := setupReadYesNo(reader, true)
	if err != nil {
		return answers, err
	}
	answers.UseVercel = useVercel

	if useVercel {
		fmt.Print("Vercel project_id: ")
		projectID, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.VercelProjectID = projectID

		fmt.Print("Vercel team_id（任意、Enter でスキップ）: ")
		teamID, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.VercelTeamID = teamID

		fmt.Print("Vercel token を ${VERCEL_TOKEN} 環境変数参照で書きますか？（推奨）(Y/n): ")
		useEnvRef, err := setupReadYesNo(reader, true)
		if err != nil {
			return answers, err
		}
		if useEnvRef {
			answers.VercelTokenRef = "${VERCEL_TOKEN}"
		} else {
			fmt.Print("Vercel token（平文でファイルに書き込みます）: ")
			token, err := setupReadLine(reader)
			if err != nil {
				return answers, err
			}
			answers.VercelTokenRef = token
			answers.HasPlainToken = true
		}
	}

	// GitHub
	fmt.Print("GitHub を設定しますか？ (Y/n): ")
	useGitHub, err := setupReadYesNo(reader, true)
	if err != nil {
		return answers, err
	}
	answers.UseGitHub = useGitHub

	if useGitHub {
		fmt.Print("GitHub repo（owner/repo 形式）: ")
		repo, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.GitHubRepo = repo

		fmt.Print("GitHub token を ${GITHUB_TOKEN} 環境変数参照で書きますか？（推奨）(Y/n): ")
		useEnvRef, err := setupReadYesNo(reader, true)
		if err != nil {
			return answers, err
		}
		if useEnvRef {
			answers.GitHubTokenRef = "${GITHUB_TOKEN}"
		} else {
			fmt.Print("GitHub token（平文でファイルに書き込みます）: ")
			token, err := setupReadLine(reader)
			if err != nil {
				return answers, err
			}
			answers.GitHubTokenRef = token
			answers.HasPlainToken = true
		}
	}

	return answers, nil
}

func setupReadLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("入力の読み込みに失敗: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func setupReadYesNo(reader *bufio.Reader, defaultYes bool) (bool, error) {
	line, err := setupReadLine(reader)
	if err != nil {
		return false, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultYes, nil
	}
	lower := strings.ToLower(line)
	return lower == "y" || lower == "yes", nil
}
