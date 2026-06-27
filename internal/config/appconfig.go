// このファイルはアプリケーション設定（認証情報・ID）の読み込みを担当する。
// 定義ファイル(env-sync.yaml)を扱う config.go とは別の概念。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// envRefRe は ${VAR} または ${VAR:-default} にマッチする正規表現。
var envRefRe = regexp.MustCompile(`\$\{([^}]+)\}`)

const projectAppConfigFile = ".env-sync.config.yaml"

// AppConfig は global / project の config ファイルからロードした認証情報・ID を保持する。
type AppConfig struct {
	Vercel AppVercelConfig `yaml:"vercel"`
	GitHub AppGitHubConfig `yaml:"github"`
}

// AppVercelConfig は Vercel の認証情報・ID。
type AppVercelConfig struct {
	Token     string `yaml:"token"`
	ProjectID string `yaml:"project_id"`
	TeamID    string `yaml:"team_id"`
}

// AppGitHubConfig は GitHub の認証情報・ID。
type AppGitHubConfig struct {
	Token string `yaml:"token"`
	Repo  string `yaml:"repo"`
}

// LoadAppConfig は global と project の設定ファイルをロードしてマージして返す。
//
// ロード元:
//   - global:  ~/.config/env-sync/config.yaml (XDG_CONFIG_HOME を尊重)
//   - project: .env-sync.config.yaml
//
// マージ優先順位: project > global。
// いずれのファイルも存在しない場合はゼロ値の AppConfig を返す。
// ファイルの YAML パースに失敗した場合はエラーを返す。
func LoadAppConfig() (*AppConfig, error) {
	global, err := loadGlobalAppConfig()
	if err != nil {
		return nil, err
	}
	project, err := loadProjectAppConfig()
	if err != nil {
		return nil, err
	}
	merged := mergeAppConfig(global, project)
	if err := expandAppConfigRefs(&merged); err != nil {
		return nil, err
	}
	return &merged, nil
}

// globalAppConfigPath は XDG_CONFIG_HOME を尊重して global config のパスを返す。
func globalAppConfigPath() string {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "env-sync", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "env-sync", "config.yaml")
}

func loadGlobalAppConfig() (AppConfig, error) {
	path := globalAppConfigPath()
	if path == "" {
		return AppConfig{}, nil
	}
	cfg, err := readAppConfigFile(path)
	if err != nil {
		return AppConfig{}, err
	}
	if cfg.Vercel.Token != "" || cfg.GitHub.Token != "" {
		warnIfInsecurePermissions(path)
	}
	return cfg, nil
}

func loadProjectAppConfig() (AppConfig, error) {
	return readAppConfigFile(projectAppConfigFile)
}

func readAppConfigFile(path string) (AppConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return AppConfig{}, nil
	}
	if err != nil {
		return AppConfig{}, fmt.Errorf("config ファイルの読み込みに失敗 (%s): %w", path, err)
	}
	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("config ファイルの YAML パースに失敗 (%s): %w", path, err)
	}
	return cfg, nil
}

// mergeAppConfig は global と project をマージする。project の非空値が優先される。
func mergeAppConfig(global, project AppConfig) AppConfig {
	merged := global
	if project.Vercel.Token != "" {
		merged.Vercel.Token = project.Vercel.Token
	}
	if project.Vercel.ProjectID != "" {
		merged.Vercel.ProjectID = project.Vercel.ProjectID
	}
	if project.Vercel.TeamID != "" {
		merged.Vercel.TeamID = project.Vercel.TeamID
	}
	if project.GitHub.Token != "" {
		merged.GitHub.Token = project.GitHub.Token
	}
	if project.GitHub.Repo != "" {
		merged.GitHub.Repo = project.GitHub.Repo
	}
	return merged
}

// expandEnvRefs は s 中の ${VAR} / ${VAR:-default} を環境変数で展開する。
// ${VAR} で VAR が未設定（空含む）かつデフォルト値がない場合はエラーを返す。
// ${:-default} のように変数名が空の場合はエラーを返す（タイポ検出）。
// ${ を含まない文字列はそのまま返す（後方互換・早期リターン）。
func expandEnvRefs(s string) (string, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	var unresolved []string
	result := envRefRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1] // "${" と "}" を除去
		if idx := strings.Index(inner, ":-"); idx >= 0 {
			varName := inner[:idx]
			if varName == "" {
				// 変数名が空の場合はタイポとして unresolved 扱い
				unresolved = append(unresolved, "(空の変数名)")
				return match
			}
			defaultVal := inner[idx+2:]
			if v := os.Getenv(varName); v != "" {
				return v
			}
			return defaultVal
		}
		if v := os.Getenv(inner); v != "" {
			return v
		}
		unresolved = append(unresolved, inner)
		return match
	})
	if len(unresolved) > 0 {
		return "", fmt.Errorf("config で参照された環境変数が未設定または書式不正です: %s", strings.Join(unresolved, ", "))
	}
	return result, nil
}

// expandAppConfigRefs は AppConfig の全文字列フィールドに環境変数展開を適用する。
func expandAppConfigRefs(cfg *AppConfig) error {
	fields := []*string{
		&cfg.Vercel.Token,
		&cfg.Vercel.ProjectID,
		&cfg.Vercel.TeamID,
		&cfg.GitHub.Token,
		&cfg.GitHub.Repo,
	}
	for _, f := range fields {
		expanded, err := expandEnvRefs(*f)
		if err != nil {
			return err
		}
		*f = expanded
	}
	return nil
}

// warnIfInsecurePermissions は global config がパーミッション 0600 でない場合に stderr へ警告する。
func warnIfInsecurePermissions(path string) {
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Mode().Perm() != 0600 {
		fmt.Fprintf(os.Stderr,
			"警告: %s にトークンが含まれていますがパーミッションが %04o です。`chmod 0600 %s` で修正することを推奨します\n",
			path, info.Mode().Perm(), path)
	}
}

// ResolveVercelToken は 環境変数 > config の優先順位で Vercel トークンを返す。
func (cfg *AppConfig) ResolveVercelToken() string {
	if v := os.Getenv("VERCEL_TOKEN"); v != "" {
		return v
	}
	return cfg.Vercel.Token
}

// ResolveVercelProjectID は 環境変数 > config の優先順位で Vercel プロジェクト ID を返す。
func (cfg *AppConfig) ResolveVercelProjectID() string {
	if v := os.Getenv("VERCEL_PROJECT_ID"); v != "" {
		return v
	}
	return cfg.Vercel.ProjectID
}

// ResolveVercelTeamID は 環境変数 > config の優先順位で Vercel チーム ID を返す。
func (cfg *AppConfig) ResolveVercelTeamID() string {
	if v := os.Getenv("VERCEL_TEAM_ID"); v != "" {
		return v
	}
	return cfg.Vercel.TeamID
}

// ResolveGitHubToken は 環境変数 > config の優先順位で GitHub トークンを返す。
func (cfg *AppConfig) ResolveGitHubToken() string {
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		return v
	}
	return cfg.GitHub.Token
}

// ResolveGitHubRepo は 環境変数 > config の優先順位で GitHub リポジトリ (owner/repo) を返す。
func (cfg *AppConfig) ResolveGitHubRepo() string {
	if v := os.Getenv("GITHUB_REPO"); v != "" {
		return v
	}
	return cfg.GitHub.Repo
}
