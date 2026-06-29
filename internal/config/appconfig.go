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

	"github.com/ptyhard/env-sync/internal/i18n"
)

// envRefRe は ${VAR} または ${VAR:-default} にマッチする正規表現。
var envRefRe = regexp.MustCompile(`\$\{([^}]+)\}`)

const projectAppConfigFile = ".env-sync.config.yaml"

// AppConfig は global / project の config ファイルからロードした認証情報・ID を保持する。
type AppConfig struct {
	Vercel   AppVercelConfig `yaml:"vercel"`
	GitHub   AppGitHubConfig `yaml:"github"`
	Language string          `yaml:"language"` // 表示言語コード（"en" / "ja"）
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
	Name            string
	ProjectID       string
	TeamID          string
	Token           string
	TokenSource     string // 取得元: "env" / "config" / ""（未設定）
	ProjectIDSource string // 取得元: "env" / "config" / "project_json" / ""（未設定）
	TeamIDSource    string // 取得元: "env" / "config" / "project_json" / ""（未設定）
}

// GitHubTarget は ResolveGitHubTargets が返す解決済みターゲット。
type GitHubTarget struct {
	// Name は config 上のターゲット名。単一解決の場合は空になる。
	Name        string
	Repo        string
	Token       string
	TokenSource string // 取得元: "env" / "config" / ""（未設定）
	RepoSource  string // 取得元: "env" / "config" / "git_remote" / ""（未設定）
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
		return AppConfig{}, fmt.Errorf("%s: %w", i18n.T(i18n.MsgConfigReadFail, path), err)
	}
	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return AppConfig{}, fmt.Errorf("%s: %w", i18n.T(i18n.MsgConfigYAMLFail, path), err)
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
	if project.Language != "" {
		merged.Language = project.Language
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
				unresolved = append(unresolved, i18n.T(i18n.MsgConfigEmptyVarName))
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
		return "", fmt.Errorf("%s", i18n.T(i18n.MsgConfigEnvRefUnset, strings.Join(unresolved, ", ")))
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
		fmt.Fprint(os.Stderr, i18n.T(i18n.MsgConfigPermWarning, path, info.Mode().Perm(), path))
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

// ResolveVercelTokenWithSource は token とその取得元 ("env"/"config"/"") を返す。
func (cfg *AppConfig) ResolveVercelTokenWithSource() (val, source string) {
	if v := os.Getenv("VERCEL_TOKEN"); v != "" {
		return v, "env"
	}
	if cfg.Vercel.Token != "" {
		return cfg.Vercel.Token, "config"
	}
	return "", ""
}

// ResolveVercelProjectIDWithSource は project ID とその取得元 ("env"/"config"/"") を返す。
func (cfg *AppConfig) ResolveVercelProjectIDWithSource() (val, source string) {
	if v := os.Getenv("VERCEL_PROJECT_ID"); v != "" {
		return v, "env"
	}
	if cfg.Vercel.ProjectID != "" {
		return cfg.Vercel.ProjectID, "config"
	}
	return "", ""
}

// ResolveVercelTeamIDWithSource は team ID とその取得元 ("env"/"config"/"") を返す。
func (cfg *AppConfig) ResolveVercelTeamIDWithSource() (val, source string) {
	if v := os.Getenv("VERCEL_TEAM_ID"); v != "" {
		return v, "env"
	}
	if cfg.Vercel.TeamID != "" {
		return cfg.Vercel.TeamID, "config"
	}
	return "", ""
}

// ResolveGitHubTokenWithSource は token とその取得元 ("env"/"config"/"") を返す。
func (cfg *AppConfig) ResolveGitHubTokenWithSource() (val, source string) {
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		return v, "env"
	}
	if cfg.GitHub.Token != "" {
		return cfg.GitHub.Token, "config"
	}
	return "", ""
}

// ResolveGitHubRepoWithSource は repo とその取得元 ("env"/"config"/"") を返す。
func (cfg *AppConfig) ResolveGitHubRepoWithSource() (val, source string) {
	if v := os.Getenv("GITHUB_REPO"); v != "" {
		return v, "env"
	}
	if cfg.GitHub.Repo != "" {
		return cfg.GitHub.Repo, "config"
	}
	return "", ""
}

// resolveVercelToken はトークンの解決優先順位（per-target > 環境変数 > top-level config）を実装する。
// perTargetToken が非空ならそれを返す。空なら top-level の ResolveVercelToken()（環境変数 > config）を使う。
func (cfg *AppConfig) resolveVercelToken(perTargetToken string) string {
	if perTargetToken != "" {
		return perTargetToken
	}
	return cfg.ResolveVercelToken()
}

// resolveVercelTokenWithSource はトークンと取得元を返す（per-target > 環境変数 > top-level config）。
func (cfg *AppConfig) resolveVercelTokenWithSource(perTargetToken string) (string, string) {
	if perTargetToken != "" {
		return perTargetToken, "config"
	}
	return cfg.ResolveVercelTokenWithSource()
}

// resolveVercelTeamIDWithSource はチーム ID と取得元を返す（per-target > 環境変数 > top-level config）。
func (cfg *AppConfig) resolveVercelTeamIDWithSource(perTargetTeamID string) (string, string) {
	if perTargetTeamID != "" {
		return perTargetTeamID, "config"
	}
	return cfg.ResolveVercelTeamIDWithSource()
}

// resolveGitHubTokenWithSource はトークンと取得元を返す（per-target > 環境変数 > top-level config）。
func (cfg *AppConfig) resolveGitHubTokenWithSource(perTargetToken string) (string, string) {
	if perTargetToken != "" {
		return perTargetToken, "config"
	}
	return cfg.ResolveGitHubTokenWithSource()
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
			return nil, fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectsNotDefined))
		}
		// 後方互換: 従来の単一解決（取得元も付与）
		tok, tokSrc := cfg.ResolveVercelTokenWithSource()
		pid, pidSrc := cfg.ResolveVercelProjectIDWithSource()
		tid, tidSrc := cfg.ResolveVercelTeamIDWithSource()
		return []VercelTarget{
			{
				ProjectID:       pid,
				ProjectIDSource: pidSrc,
				TeamID:          tid,
				TeamIDSource:    tidSrc,
				Token:           tok,
				TokenSource:     tokSrc,
			},
		}, nil
	}

	// name・project_id の必須チェックと重複チェック（selectName の有無によらず常に実施）。
	// projects が定義されている場合は .vercel/project.json フォールバックを行わないため
	// 各エントリの project_id が必須となる（絞り込み後の件数に関わらず全エントリを検証する）。
	if err := validateVercelProjectConfs(cfg.Vercel.Projects); err != nil {
		return nil, err
	}

	var targets []VercelTarget
	for _, p := range cfg.Vercel.Projects {
		if selectName != "" && p.Name != selectName {
			continue
		}
		tok, tokSrc := cfg.resolveVercelTokenWithSource(p.Token)
		tid, tidSrc := cfg.resolveVercelTeamIDWithSource(p.TeamID)
		targets = append(targets, VercelTarget{
			Name:            p.Name,
			ProjectID:       p.ProjectID,
			ProjectIDSource: "config",
			TeamID:          tid,
			TeamIDSource:    tidSrc,
			Token:           tok,
			TokenSource:     tokSrc,
		})
	}

	if selectName != "" && len(targets) == 0 {
		return nil, fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectNameNotFound, selectName))
	}
	return targets, nil
}

// validateVercelProjectConfs は vercel.projects の name 必須・重複チェックおよび
// project_id 必須チェックを行う。projects が定義されている場合は .vercel/project.json
// フォールバックを使わないため、絞り込み後の件数に関わらず全エントリの project_id が必須。
func validateVercelProjectConfs(projects []VercelProjectConf) error {
	seen := make(map[string]bool, len(projects))
	for _, p := range projects {
		if p.Name == "" {
			return fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectNameRequired))
		}
		if seen[p.Name] {
			return fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectNameDuplicate, p.Name))
		}
		seen[p.Name] = true
		if p.ProjectID == "" {
			return fmt.Errorf("%s", i18n.T(i18n.MsgVercelProjectIDRequired, p.Name))
		}
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
			return nil, fmt.Errorf("%s", i18n.T(i18n.MsgGitHubReposNotDefined))
		}
		// 後方互換: 従来の単一解決（取得元も付与）
		tok, tokSrc := cfg.ResolveGitHubTokenWithSource()
		repo, repoSrc := cfg.ResolveGitHubRepoWithSource()
		return []GitHubTarget{
			{
				Repo:        repo,
				RepoSource:  repoSrc,
				Token:       tok,
				TokenSource: tokSrc,
			},
		}, nil
	}

	// name・repo の必須チェックと重複チェック（selectName の有無によらず常に実施）。
	// repos が定義されている場合は git remote / 環境変数フォールバックを行わないため
	// 各エントリの repo が必須となる（絞り込み後の件数に関わらず全エントリを検証する）。
	if err := validateGitHubRepoConfs(cfg.GitHub.Repos); err != nil {
		return nil, err
	}

	var targets []GitHubTarget
	for _, r := range cfg.GitHub.Repos {
		if selectName != "" && r.Name != selectName {
			continue
		}
		tok, tokSrc := cfg.resolveGitHubTokenWithSource(r.Token)
		targets = append(targets, GitHubTarget{
			Name:        r.Name,
			Repo:        r.Repo,
			RepoSource:  "config",
			Token:       tok,
			TokenSource: tokSrc,
		})
	}

	if selectName != "" && len(targets) == 0 {
		return nil, fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoNameNotFound, selectName))
	}
	return targets, nil
}

// validateGitHubRepoConfs は github.repos の name 必須・重複チェックおよび
// repo 必須チェックを行う。repos が定義されている場合は git remote フォールバックを使わないため
// 絞り込み後の件数に関わらず全エントリの repo が必須。
func validateGitHubRepoConfs(repos []GitHubRepoConf) error {
	seen := make(map[string]bool, len(repos))
	for _, r := range repos {
		if r.Name == "" {
			return fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoNameRequired))
		}
		if seen[r.Name] {
			return fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoNameDuplicate, r.Name))
		}
		seen[r.Name] = true
		if r.Repo == "" {
			return fmt.Errorf("%s", i18n.T(i18n.MsgGitHubRepoRepoRequired, r.Name))
		}
	}
	return nil
}
