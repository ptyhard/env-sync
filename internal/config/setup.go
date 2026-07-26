package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/ptyhard/env-sync/internal/i18n"
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

	UseCloudflare       bool
	CloudflareAccountID string
	CloudflareScript    string // Worker スクリプト名（空なら wrangler 設定から解決）
	CloudflareTokenRef  string // "${CLOUDFLARE_API_TOKEN}" または平文トークン

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
		case arg == "--lang" || arg == "-lang" || arg == "--language" || arg == "-language":
			// 言語フラグは main.go でプレスキャン済みのため値を読み飛ばす。
			i++
		case strings.HasPrefix(arg, "--lang=") || strings.HasPrefix(arg, "--language="):
			// 言語フラグは main.go でプレスキャン済みのためスキップする。
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

// yamlSingleQuote は value を YAML シングルクォートスカラとしてエスケープして返す。
// シングルクォートスカラ内では ' のみ ” （シングルクォート2つ）に置換すればよく、
// :、#、$、改行などの特殊文字を含む値でも安全に扱える。
func yamlSingleQuote(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}

// BuildSetupYAML は SetupAnswers から認証情報 config の YAML テキストを生成する。
// 生成される YAML は AppConfig スキーマに準拠し LoadAppConfig() でパースできる。
// ユーザー入力値は YAML シングルクォートスカラとして安全にエスケープして出力する。
func BuildSetupYAML(answers SetupAnswers) string {
	var sb strings.Builder
	sb.WriteString(i18n.T(i18n.MsgSetupYAMLHeader))

	if answers.UseVercel {
		sb.WriteString("vercel:\n")
		sb.WriteString("  token: ")
		sb.WriteString(yamlSingleQuote(answers.VercelTokenRef))
		sb.WriteString("\n")
		if answers.VercelProjectID != "" {
			sb.WriteString("  project_id: ")
			sb.WriteString(yamlSingleQuote(answers.VercelProjectID))
			sb.WriteString("\n")
		}
		if answers.VercelTeamID != "" {
			sb.WriteString("  team_id: ")
			sb.WriteString(yamlSingleQuote(answers.VercelTeamID))
			sb.WriteString("\n")
		}
	}

	if answers.UseGitHub {
		sb.WriteString("github:\n")
		sb.WriteString("  token: ")
		sb.WriteString(yamlSingleQuote(answers.GitHubTokenRef))
		sb.WriteString("\n")
		if answers.GitHubRepo != "" {
			sb.WriteString("  repo: ")
			sb.WriteString(yamlSingleQuote(answers.GitHubRepo))
			sb.WriteString("\n")
		}
	}

	if answers.UseCloudflare {
		sb.WriteString("cloudflare:\n")
		sb.WriteString("  api_token: ")
		sb.WriteString(yamlSingleQuote(answers.CloudflareTokenRef))
		sb.WriteString("\n")
		if answers.CloudflareAccountID != "" {
			sb.WriteString("  account_id: ")
			sb.WriteString(yamlSingleQuote(answers.CloudflareAccountID))
			sb.WriteString("\n")
		}
		if answers.CloudflareScript != "" {
			sb.WriteString("  script: ")
			sb.WriteString(yamlSingleQuote(answers.CloudflareScript))
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

// WriteSetupFile は path へ content を書き込む。
// force なしで既存ファイルがある場合はエラーを返す。
// --force で既存ファイルを上書きする場合も os.Chmod で perm を確実に適用する。
func WriteSetupFile(path, content string, perm os.FileMode, force bool) error {
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if !force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(path, flags, perm)
	if err != nil {
		if !force && os.IsExist(err) {
			return fmt.Errorf("%s", i18n.T(i18n.MsgFileExists, path))
		}
		return fmt.Errorf("%s", i18n.T(i18n.MsgSetupConfigWriteFail, path, err))
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return fmt.Errorf("%s", i18n.T(i18n.MsgSetupConfigWriteFail, path, err))
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.MsgSetupConfigWriteFail, path, err))
	}
	// --force 上書き時は os.OpenFile の perm が既存ファイルに適用されないため
	// 書き込み完了後に明示的にパーミッションを設定する。
	if err := os.Chmod(path, perm); err != nil {
		return fmt.Errorf("%s", i18n.T(i18n.MsgSetupConfigChmodFail, path, err))
	}
	return nil
}

// RunSetup は setup サブコマンドのメイン処理。非 TTY 環境ではエラーで停止する。
func RunSetup(argv []string, printUsageFn func()) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("%s", i18n.T(i18n.MsgSetupNonInteractive))
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
			return fmt.Errorf("%s", i18n.T(i18n.MsgSetupHomeDirFail))
		}
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("%s", i18n.T(i18n.MsgSetupDirCreateFail, dir, err))
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

	fmt.Print(i18n.T(i18n.MsgGenerated, outputPath))
	if answers.HasPlainToken || opts.Global {
		fmt.Println(i18n.T(i18n.MsgSetupTokenNote))
	}
	fmt.Println()
	fmt.Println(i18n.T(i18n.MsgSetupGitignoreNote))

	return nil
}

func promptSetupAnswers(reader *bufio.Reader) (SetupAnswers, error) {
	var answers SetupAnswers

	// Vercel
	fmt.Print(i18n.T(i18n.MsgSetupAskVercel))
	useVercel, err := setupReadYesNo(reader, true)
	if err != nil {
		return answers, err
	}
	answers.UseVercel = useVercel

	if useVercel {
		fmt.Print(i18n.T(i18n.MsgSetupVercelProjectID))
		projectID, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.VercelProjectID = projectID

		fmt.Print(i18n.T(i18n.MsgSetupVercelTeamID))
		teamID, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.VercelTeamID = teamID

		fmt.Print(i18n.T(i18n.MsgSetupVercelTokenEnvRef))
		useEnvRef, err := setupReadYesNo(reader, true)
		if err != nil {
			return answers, err
		}
		if useEnvRef {
			answers.VercelTokenRef = "${VERCEL_TOKEN}"
		} else {
			fmt.Print(i18n.T(i18n.MsgSetupVercelTokenPlain))
			token, err := setupReadLine(reader)
			if err != nil {
				return answers, err
			}
			answers.VercelTokenRef = token
			answers.HasPlainToken = true
		}
	}

	// GitHub
	fmt.Print(i18n.T(i18n.MsgSetupAskGitHub))
	useGitHub, err := setupReadYesNo(reader, true)
	if err != nil {
		return answers, err
	}
	answers.UseGitHub = useGitHub

	if useGitHub {
		fmt.Print(i18n.T(i18n.MsgSetupGitHubRepo))
		repo, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.GitHubRepo = repo

		fmt.Print(i18n.T(i18n.MsgSetupGitHubTokenEnvRef))
		useEnvRef, err := setupReadYesNo(reader, true)
		if err != nil {
			return answers, err
		}
		if useEnvRef {
			answers.GitHubTokenRef = "${GITHUB_TOKEN}"
		} else {
			fmt.Print(i18n.T(i18n.MsgSetupGitHubTokenPlain))
			token, err := setupReadLine(reader)
			if err != nil {
				return answers, err
			}
			answers.GitHubTokenRef = token
			answers.HasPlainToken = true
		}
	}

	// Cloudflare Workers
	fmt.Print(i18n.T(i18n.MsgSetupAskCloudflare))
	useCloudflare, err := setupReadYesNo(reader, true)
	if err != nil {
		return answers, err
	}
	answers.UseCloudflare = useCloudflare

	if useCloudflare {
		fmt.Print(i18n.T(i18n.MsgSetupCloudflareAccountID))
		accountID, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.CloudflareAccountID = accountID

		fmt.Print(i18n.T(i18n.MsgSetupCloudflareScript))
		script, err := setupReadLine(reader)
		if err != nil {
			return answers, err
		}
		answers.CloudflareScript = script

		fmt.Print(i18n.T(i18n.MsgSetupCloudflareTokenEnvRef))
		useEnvRef, err := setupReadYesNo(reader, true)
		if err != nil {
			return answers, err
		}
		if useEnvRef {
			answers.CloudflareTokenRef = "${CLOUDFLARE_API_TOKEN}"
		} else {
			fmt.Print(i18n.T(i18n.MsgSetupCloudflareTokenPlain))
			token, err := setupReadLine(reader)
			if err != nil {
				return answers, err
			}
			answers.CloudflareTokenRef = token
			answers.HasPlainToken = true
		}
	}

	return answers, nil
}

func setupReadLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("%s: %w", i18n.T(i18n.MsgSetupInputReadFail), err)
	}
	// 改行文字のほか前後の空白も除去する。
	// 末尾スペース等が project_id / repo / token に混入すると認証や ID 解決が失敗しやすいため。
	return strings.TrimSpace(line), nil
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
