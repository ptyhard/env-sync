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

// VercelProjectConf は vercel.projects の 1 件分の設定。
type VercelProjectConf struct {
	Name      string `yaml:"name"`
	ProjectID string `yaml:"project_id"`
	TeamID    string `yaml:"team_id"`
	Token     string `yaml:"token"`
}

// GitHubRepoConf は github.repos の 1 件分の設定。
type GitHubRepoConf struct {
	Name  string `yaml:"name"`
	Repo  string `yaml:"repo"`
	Token string `yaml:"token"`
}

// VercelTarget は ResolveVercelTargets が返す解決済みターゲット。
type VercelTarget struct {
	// Name は config 上のターゲット名。単一解決の場合は空になる。
	Name      string
	ProjectID string
	TeamID    string
	Token     string
}

// GitHubTarget は ResolveGitHubTargets が返す解決済みターゲット。
type GitHubTarget struct {
	// Name は config 上のターゲット名。単一解決の場合は空になる。
	Name  string
	Repo  string
	Token string
}

// AppVercelConfig は Vercel の認証情報・ID。
type AppVercelConfig struct {
	Token     string              `yaml:"token"`
	ProjectID string              `yaml:"project_id"`
	TeamID    string              `yaml:"team_id"`
	Projects  []VercelProjectConf `yaml:"projects"`
}

// AppGitHubConfig は GitHub の認証情報・ID。
type AppGitHubConfig struct {
	Token string           `yaml:"token"`
	Repo  string           `yaml:"repo"`
	Repos []GitHubRepoConf `yaml:"repos"`
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
	if cfg.Vercel.Token != "" || cfg.GitHub.Token != "" || hasPerTargetToken(cfg) {
		warnIfInsecurePermissions(path)
	}
	return cfg, nil
}

// hasPerTargetToken は per-target の token が 1 件以上あるかを返す。
func hasPerTargetToken(cfg AppConfig) bool {
	for _, p := range cfg.Vercel.Projects {
		if p.Token != "" {
			return true
		}
	}
	for _, r := range cfg.GitHub.Repos {
		if r.Token != "" {
			return true
		}
	}
	return false
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
// projects/repos は project 側が非空なら global を置き換える（要素単位マージはしない）。
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
	if len(project.Vercel.Projects) > 0 {
		merged.Vercel.Projects = project.Vercel.Projects
	}
	if project.GitHub.Token != "" {
		merged.GitHub.Token = project.GitHub.Token
	}
	if project.GitHub.Repo != "" {
		merged.GitHub.Repo = project.GitHub.Repo
	}
	if len(project.GitHub.Repos) > 0 {
		merged.GitHub.Repos = project.GitHub.Repos
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
	// per-target の文字列フィールドも展開する
	for i := range cfg.Vercel.Projects {
		pfields := []*string{
			&cfg.Vercel.Projects[i].Token,
			&cfg.Vercel.Projects[i].ProjectID,
			&cfg.Vercel.Projects[i].TeamID,
		}
		for _, f := range pfields {
			expanded, err := expandEnvRefs(*f)
			if err != nil {
				return err
			}
			*f = expanded
		}
	}
	for i := range cfg.GitHub.Repos {
		rfields := []*string{
			&cfg.GitHub.Repos[i].Token,
			&cfg.GitHub.Repos[i].Repo,
		}
		for _, f := range rfields {
			expanded, err := expandEnvRefs(*f)
			if err != nil {
				return err
			}
			*f = expanded
		}
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

// resolveVercelToken はトークンの解決優先順位（per-target > 環境変数 > top-level config）を実装する。
// perTargetToken が非空ならそれを返す。空なら top-level の ResolveVercelToken()（環境変数 > config）を使う。
func (cfg *AppConfig) resolveVercelToken(perTargetToken string) string {
	if perTargetToken != "" {
		return perTargetToken
	}
	return cfg.ResolveVercelToken()
}

// resolveVercelTeamID はチーム ID の解決優先順位（per-target > 環境変数 > top-level config）を実装する。
// perTargetTeamID が非空ならそれを返す。空なら ResolveVercelTeamID()（環境変数 > config）を使う。
func (cfg *AppConfig) resolveVercelTeamID(perTargetTeamID string) string {
	if perTargetTeamID != "" {
		return perTargetTeamID
	}
	return cfg.ResolveVercelTeamID()
}

// resolveGitHubToken はトークンの解決優先順位（per-target > 環境変数 > top-level config）を実装する。
// perTargetToken が非空ならそれを返す。空なら ResolveGitHubToken()（環境変数 > config）を使う。
func (cfg *AppConfig) resolveGitHubToken(perTargetToken string) string {
	if perTargetToken != "" {
		return perTargetToken
	}
	return cfg.ResolveGitHubToken()
}

// ResolveVercelTargets は vercel.projects の設定から解決済み VercelTarget スライスを返す。
//
// selectName が空でない場合は name 一致の 1 件のみ返す。一致しない場合はエラー。
// projects が未定義（空スライス）の場合は後方互換として単一解決（環境変数 > config）で 1 件返す。
// ただし selectName が指定されているのに projects が未定義の場合はエラーを返す。
func (cfg *AppConfig) ResolveVercelTargets(selectName string) ([]VercelTarget, error) {
	if len(cfg.Vercel.Projects) == 0 {
		// selectName が指定されているのに projects が未定義は設定ミスのためエラー
		if selectName != "" {
			return nil, fmt.Errorf("--vercel-project が指定されましたが config に vercel.projects が定義されていません")
		}
		// 後方互換: 従来の単一解決
		return []VercelTarget{
			{
				ProjectID: cfg.ResolveVercelProjectID(),
				TeamID:    cfg.ResolveVercelTeamID(),
				Token:     cfg.ResolveVercelToken(),
			},
		}, nil
	}

	// name の必須チェックと重複チェック（selectName の有無によらず常に実施）
	if err := validateVercelProjectNames(cfg.Vercel.Projects); err != nil {
		return nil, err
	}

	var targets []VercelTarget
	for _, p := range cfg.Vercel.Projects {
		if selectName != "" && p.Name != selectName {
			continue
		}
		targets = append(targets, VercelTarget{
			Name:      p.Name,
			ProjectID: p.ProjectID,
			TeamID:    cfg.resolveVercelTeamID(p.TeamID),
			Token:     cfg.resolveVercelToken(p.Token),
		})
	}

	if selectName != "" && len(targets) == 0 {
		return nil, fmt.Errorf("指定されたプロジェクト名 %q が config に定義されていません", selectName)
	}
	return targets, nil
}

// validateVercelProjectNames は vercel.projects の name 必須チェックと重複チェックを行う。
func validateVercelProjectNames(projects []VercelProjectConf) error {
	seen := make(map[string]bool, len(projects))
	for _, p := range projects {
		if p.Name == "" {
			return fmt.Errorf("vercel.projects の各エントリには name が必須です")
		}
		if seen[p.Name] {
			return fmt.Errorf("vercel.projects の name %q が重複しています", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

// ResolveGitHubTargets は github.repos の設定から解決済み GitHubTarget スライスを返す。
//
// selectName が空でない場合は name 一致の 1 件のみ返す。一致しない場合はエラー。
// repos が未定義（空スライス）の場合は後方互換として単一解決（環境変数 > config）で 1 件返す。
// ただし selectName が指定されているのに repos が未定義の場合はエラーを返す。
func (cfg *AppConfig) ResolveGitHubTargets(selectName string) ([]GitHubTarget, error) {
	if len(cfg.GitHub.Repos) == 0 {
		// selectName が指定されているのに repos が未定義は設定ミスのためエラー
		if selectName != "" {
			return nil, fmt.Errorf("--github-repo が指定されましたが config に github.repos が定義されていません")
		}
		// 後方互換: 従来の単一解決
		return []GitHubTarget{
			{
				Repo:  cfg.ResolveGitHubRepo(),
				Token: cfg.ResolveGitHubToken(),
			},
		}, nil
	}

	// name の必須チェックと重複チェック（selectName の有無によらず常に実施）
	if err := validateGitHubRepoNames(cfg.GitHub.Repos); err != nil {
		return nil, err
	}

	var targets []GitHubTarget
	for _, r := range cfg.GitHub.Repos {
		if selectName != "" && r.Name != selectName {
			continue
		}
		targets = append(targets, GitHubTarget{
			Name:  r.Name,
			Repo:  r.Repo,
			Token: cfg.resolveGitHubToken(r.Token),
		})
	}

	if selectName != "" && len(targets) == 0 {
		return nil, fmt.Errorf("指定されたリポジトリ名 %q が config に定義されていません", selectName)
	}
	return targets, nil
}

// validateGitHubRepoNames は github.repos の name 必須チェックと重複チェックを行う。
func validateGitHubRepoNames(repos []GitHubRepoConf) error {
	seen := make(map[string]bool, len(repos))
	for _, r := range repos {
		if r.Name == "" {
			return fmt.Errorf("github.repos の各エントリには name が必須です")
		}
		if seen[r.Name] {
			return fmt.Errorf("github.repos の name %q が重複しています", r.Name)
		}
		seen[r.Name] = true
	}
	return nil
}
