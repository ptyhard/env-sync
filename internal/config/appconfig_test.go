package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ptyhard/env-sync/internal/config"
)

// chdirCleanup は os.Chdir でカレントディレクトリを変更し、
// テスト終了時に元のディレクトリへ戻す t.Cleanup を登録する。
func chdirCleanup(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(orig); err != nil {
			t.Errorf("元ディレクトリへの復元に失敗: %v", err)
		}
	})
}

// --- LoadAppConfig / マージ / 優先順位テスト ---

func TestLoadAppConfig_NoFiles_ReturnsZero(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	chdirCleanup(t, dir)
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "" || cfg.Vercel.ProjectID != "" || cfg.GitHub.Token != "" {
		t.Errorf("ゼロ値を期待したが非空フィールドがある: %+v", cfg)
	}
}

func TestLoadAppConfig_GlobalOnly(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "env-sync")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(globalDir, "config.yaml")
	if err := os.WriteFile(globalPath, []byte(`
vercel:
  token: global-token
  project_id: global-pid
github:
  token: global-gh-token
  repo: global/repo
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	// project config が存在しないディレクトリで実行
	projectDir := t.TempDir()
	chdirCleanup(t, projectDir)
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "global-token" {
		t.Errorf("Vercel.Token = %q, want global-token", cfg.Vercel.Token)
	}
	if cfg.Vercel.ProjectID != "global-pid" {
		t.Errorf("Vercel.ProjectID = %q, want global-pid", cfg.Vercel.ProjectID)
	}
	if cfg.GitHub.Token != "global-gh-token" {
		t.Errorf("GitHub.Token = %q, want global-gh-token", cfg.GitHub.Token)
	}
	if cfg.GitHub.Repo != "global/repo" {
		t.Errorf("GitHub.Repo = %q, want global/repo", cfg.GitHub.Repo)
	}
}

func TestLoadAppConfig_ProjectOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-such-xdg"))
	chdirCleanup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: project-token
  team_id: project-team
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "project-token" {
		t.Errorf("Vercel.Token = %q, want project-token", cfg.Vercel.Token)
	}
	if cfg.Vercel.TeamID != "project-team" {
		t.Errorf("Vercel.TeamID = %q, want project-team", cfg.Vercel.TeamID)
	}
}

func TestLoadAppConfig_ProjectOverridesGlobal(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "env-sync")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`
vercel:
  token: global-token
  project_id: global-pid
github:
  token: global-gh
  repo: global/repo
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)

	projectDir := t.TempDir()
	chdirCleanup(t, projectDir)
	if err := os.WriteFile(filepath.Join(projectDir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: project-token
github:
  repo: project/repo
`), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	// project が global を上書き
	if cfg.Vercel.Token != "project-token" {
		t.Errorf("Vercel.Token = %q, want project-token (project should override global)", cfg.Vercel.Token)
	}
	// project にない値は global が残る
	if cfg.Vercel.ProjectID != "global-pid" {
		t.Errorf("Vercel.ProjectID = %q, want global-pid (global fallback)", cfg.Vercel.ProjectID)
	}
	if cfg.GitHub.Token != "global-gh" {
		t.Errorf("GitHub.Token = %q, want global-gh (global fallback)", cfg.GitHub.Token)
	}
	if cfg.GitHub.Repo != "project/repo" {
		t.Errorf("GitHub.Repo = %q, want project/repo (project should override)", cfg.GitHub.Repo)
	}
}

// --- 解決優先順位: 環境変数 > project config > global config ---

func TestResolveVercelToken_EnvBeatsConfig(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "env-token")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "config-token"
	if got := cfg.ResolveVercelToken(); got != "env-token" {
		t.Errorf("ResolveVercelToken = %q, want env-token (env should win)", got)
	}
}

func TestResolveVercelToken_FallsBackToConfig(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "config-token"
	if got := cfg.ResolveVercelToken(); got != "config-token" {
		t.Errorf("ResolveVercelToken = %q, want config-token", got)
	}
}

func TestResolveVercelProjectID_EnvBeatsConfig(t *testing.T) {
	t.Setenv("VERCEL_PROJECT_ID", "env-pid")
	cfg := &config.AppConfig{}
	cfg.Vercel.ProjectID = "config-pid"
	if got := cfg.ResolveVercelProjectID(); got != "env-pid" {
		t.Errorf("ResolveVercelProjectID = %q, want env-pid", got)
	}
}

func TestResolveVercelTeamID_EnvBeatsConfig(t *testing.T) {
	t.Setenv("VERCEL_TEAM_ID", "env-team")
	cfg := &config.AppConfig{}
	cfg.Vercel.TeamID = "config-team"
	if got := cfg.ResolveVercelTeamID(); got != "env-team" {
		t.Errorf("ResolveVercelTeamID = %q, want env-team", got)
	}
}

func TestResolveGitHubToken_EnvBeatsConfig(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-gh-token")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "config-gh-token"
	if got := cfg.ResolveGitHubToken(); got != "env-gh-token" {
		t.Errorf("ResolveGitHubToken = %q, want env-gh-token", got)
	}
}

func TestResolveGitHubRepo_EnvBeatsConfig(t *testing.T) {
	t.Setenv("GITHUB_REPO", "env/repo")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repo = "config/repo"
	if got := cfg.ResolveGitHubRepo(); got != "env/repo" {
		t.Errorf("ResolveGitHubRepo = %q, want env/repo", got)
	}
}

func TestResolveGitHubRepo_FallsBackToConfig(t *testing.T) {
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repo = "config/repo"
	if got := cfg.ResolveGitHubRepo(); got != "config/repo" {
		t.Errorf("ResolveGitHubRepo = %q, want config/repo", got)
	}
}

// --- 環境変数参照展開テスト ---

func TestLoadAppConfig_EnvRefExpansion_Basic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	t.Setenv("MY_TOKEN", "abc123")
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: ${MY_TOKEN}
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "abc123" {
		t.Errorf("Vercel.Token = %q, want abc123", cfg.Vercel.Token)
	}
}

func TestLoadAppConfig_EnvRefExpansion_Default(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	t.Setenv("UNDEFINED_VAR", "")
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: ${UNDEFINED_VAR:-fallback}
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "fallback" {
		t.Errorf("Vercel.Token = %q, want fallback", cfg.Vercel.Token)
	}
}

func TestLoadAppConfig_EnvRefExpansion_UndefinedError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	t.Setenv("UNDEFINED_VAR", "")
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: ${UNDEFINED_VAR}
`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadAppConfig()
	if err == nil {
		t.Fatal("未設定変数参照に対してエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "UNDEFINED_VAR") {
		t.Errorf("エラーメッセージに変数名が含まれることを期待: %v", err)
	}
}

func TestLoadAppConfig_EnvRefExpansion_BackwardCompat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: plain-token
  project_id: plain-pid
github:
  repo: owner/repo
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "plain-token" {
		t.Errorf("Vercel.Token = %q, want plain-token", cfg.Vercel.Token)
	}
	if cfg.Vercel.ProjectID != "plain-pid" {
		t.Errorf("Vercel.ProjectID = %q, want plain-pid", cfg.Vercel.ProjectID)
	}
	if cfg.GitHub.Repo != "owner/repo" {
		t.Errorf("GitHub.Repo = %q, want owner/repo", cfg.GitHub.Repo)
	}
}

func TestLoadAppConfig_EnvRefExpansion_AllFields(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	t.Setenv("V_TOKEN", "vtoken")
	t.Setenv("V_PID", "vpid")
	t.Setenv("V_TEAM", "vteam")
	t.Setenv("GH_TOKEN", "ghtoken")
	t.Setenv("GH_REPO", "owner/repo")
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: ${V_TOKEN}
  project_id: ${V_PID}
  team_id: ${V_TEAM}
github:
  token: ${GH_TOKEN}
  repo: ${GH_REPO}
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Token != "vtoken" {
		t.Errorf("Vercel.Token = %q, want vtoken", cfg.Vercel.Token)
	}
	if cfg.Vercel.ProjectID != "vpid" {
		t.Errorf("Vercel.ProjectID = %q, want vpid", cfg.Vercel.ProjectID)
	}
	if cfg.Vercel.TeamID != "vteam" {
		t.Errorf("Vercel.TeamID = %q, want vteam", cfg.Vercel.TeamID)
	}
	if cfg.GitHub.Token != "ghtoken" {
		t.Errorf("GitHub.Token = %q, want ghtoken", cfg.GitHub.Token)
	}
	if cfg.GitHub.Repo != "owner/repo" {
		t.Errorf("GitHub.Repo = %q, want owner/repo", cfg.GitHub.Repo)
	}
}

func TestLoadAppConfig_EnvRefExpansion_AltNameBeatsDirectEnv(t *testing.T) {
	// 別名参照: MY_TOKEN=abc + token: ${MY_TOKEN} で abc が解決される
	// （同名 VERCEL_TOKEN とは別の変数を使う）
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	t.Setenv("MY_VERCEL_TOKEN", "my-alt-token")
	t.Setenv("VERCEL_TOKEN", "")
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: ${MY_VERCEL_TOKEN}
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	// ResolveVercelToken は VERCEL_TOKEN が空なので config の展開済み値を返す
	if got := cfg.ResolveVercelToken(); got != "my-alt-token" {
		t.Errorf("ResolveVercelToken = %q, want my-alt-token", got)
	}
}

// --- ファイル不在時の後方互換テスト ---

func TestLoadAppConfig_BackwardCompat_NoConfigFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "absent"))
	chdirCleanup(t, dir)
	// 環境変数のみで動作するか確認（エラーなし）
	t.Setenv("VERCEL_TOKEN", "env-only-token")
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("config ファイルなしでもエラーなしを期待: %v", err)
	}
	if got := cfg.ResolveVercelToken(); got != "env-only-token" {
		t.Errorf("ResolveVercelToken = %q, want env-only-token", got)
	}
}

// --- パース失敗テスト ---

func TestLoadAppConfig_InvalidYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: [invalid yaml
`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadAppConfig()
	if err == nil {
		t.Fatal("無効な YAML に対してエラーを期待したが nil")
	}
}

// --- パーミッション警告テスト (stderr への出力確認) ---

func TestLoadAppConfig_GlobalPermWarning_WritesToStderr(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "env-sync")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(globalDir, "config.yaml")
	// パーミッション 0644（0600 でない）でトークンを書く
	if err := os.WriteFile(globalPath, []byte(`
vercel:
  token: secret-token
`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	projectDir := t.TempDir()
	chdirCleanup(t, projectDir)

	// stderr をキャプチャして警告を確認
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	_, loadErr := config.LoadAppConfig()

	// LoadAppConfig 呼び出し直後に復元し、並列実行時の stdout/stderr 混入を防ぐ
	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()
	output := string(buf[:n])

	if loadErr != nil {
		t.Fatalf("エラーなしを期待: %v", loadErr)
	}
	if output == "" {
		t.Error("パーミッション警告が stderr に出力されることを期待したが何も出力されなかった")
	}
}

func TestLoadAppConfig_GlobalPermOK_NoWarning(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "env-sync")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	globalPath := filepath.Join(globalDir, "config.yaml")
	// パーミッション 0600（正しい）
	if err := os.WriteFile(globalPath, []byte(`
vercel:
  token: secret-token
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	projectDir := t.TempDir()
	chdirCleanup(t, projectDir)

	// stderr をキャプチャして警告がないことを確認
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	_, loadErr := config.LoadAppConfig()

	// LoadAppConfig 呼び出し直後に復元し、並列実行時の stdout/stderr 混入を防ぐ
	w.Close()
	os.Stderr = origStderr

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	r.Close()
	output := string(buf[:n])

	if loadErr != nil {
		t.Fatalf("エラーなしを期待: %v", loadErr)
	}
	if output != "" {
		t.Errorf("0600 の場合は警告不要だが出力あり: %q", output)
	}
}

// --- 空変数名バリデーションテスト ---

func TestLoadAppConfig_EnvRefExpansion_EmptyVarNameError(t *testing.T) {
	// ${:-fallback} のように変数名が空の場合はエラーを期待（タイポ検出）
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: ${:-fallback}
`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := config.LoadAppConfig()
	if err == nil {
		t.Fatal("空変数名参照に対してエラーを期待したが nil")
	}
}

// --- モノレポ対応: projects / repos のロード・マージ・解決テスト ---

func TestLoadAppConfig_VercelProjects_Load(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: global-token
  team_id: global-team
  projects:
    - name: app-a
      project_id: pid-a
    - name: app-b
      project_id: pid-b
      token: per-b-token
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(cfg.Vercel.Projects) != 2 {
		t.Fatalf("Projects len = %d, want 2", len(cfg.Vercel.Projects))
	}
	if cfg.Vercel.Projects[0].Name != "app-a" {
		t.Errorf("Projects[0].Name = %q, want app-a", cfg.Vercel.Projects[0].Name)
	}
	if cfg.Vercel.Projects[1].Token != "per-b-token" {
		t.Errorf("Projects[1].Token = %q, want per-b-token", cfg.Vercel.Projects[1].Token)
	}
}

func TestLoadAppConfig_GitHubRepos_Load(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
github:
  token: global-gh-token
  repos:
    - name: frontend
      repo: org/frontend
    - name: backend
      repo: org/backend
      token: per-backend-token
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(cfg.GitHub.Repos) != 2 {
		t.Fatalf("Repos len = %d, want 2", len(cfg.GitHub.Repos))
	}
	if cfg.GitHub.Repos[0].Repo != "org/frontend" {
		t.Errorf("Repos[0].Repo = %q, want org/frontend", cfg.GitHub.Repos[0].Repo)
	}
	if cfg.GitHub.Repos[1].Token != "per-backend-token" {
		t.Errorf("Repos[1].Token = %q, want per-backend-token", cfg.GitHub.Repos[1].Token)
	}
}

func TestLoadAppConfig_VercelProjects_MergeReplace(t *testing.T) {
	// global に projects があり、project にも projects がある場合は project 側が置き換える
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "env-sync")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`
vercel:
  projects:
    - name: global-app
      project_id: global-pid
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	projectDir := t.TempDir()
	chdirCleanup(t, projectDir)
	if err := os.WriteFile(filepath.Join(projectDir, ".env-sync.config.yaml"), []byte(`
vercel:
  projects:
    - name: project-app
      project_id: project-pid
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	// project 側の projects が global を置き換える
	if len(cfg.Vercel.Projects) != 1 {
		t.Fatalf("Projects len = %d, want 1 (project side should replace global)", len(cfg.Vercel.Projects))
	}
	if cfg.Vercel.Projects[0].Name != "project-app" {
		t.Errorf("Projects[0].Name = %q, want project-app", cfg.Vercel.Projects[0].Name)
	}
}

func TestLoadAppConfig_VercelProjects_MergeGlobalWhenProjectEmpty(t *testing.T) {
	// project 側 projects が空なら global を残す
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "env-sync")
	if err := os.MkdirAll(globalDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.yaml"), []byte(`
vercel:
  projects:
    - name: global-app
      project_id: global-pid
`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	projectDir := t.TempDir()
	chdirCleanup(t, projectDir)
	// project config には projects を書かない
	if err := os.WriteFile(filepath.Join(projectDir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: project-token
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	// project 側 projects が空なので global の projects が残る
	if len(cfg.Vercel.Projects) != 1 {
		t.Fatalf("Projects len = %d, want 1 (global projects should remain)", len(cfg.Vercel.Projects))
	}
	if cfg.Vercel.Projects[0].Name != "global-app" {
		t.Errorf("Projects[0].Name = %q, want global-app", cfg.Vercel.Projects[0].Name)
	}
}

func TestResolveVercelTargets_AllProjects(t *testing.T) {
	// projects が定義されている場合、全件を VercelTarget として返す
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "top-token"
	cfg.Vercel.TeamID = "top-team"
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
		{Name: "app-b", ProjectID: "pid-b", Token: "per-b-token", TeamID: "per-b-team"},
	}
	targets, err := cfg.ResolveVercelTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(targets))
	}
	// app-a: token は top-level フォールバック
	if targets[0].Token != "top-token" {
		t.Errorf("targets[0].Token = %q, want top-token (top-level fallback)", targets[0].Token)
	}
	if targets[0].TeamID != "top-team" {
		t.Errorf("targets[0].TeamID = %q, want top-team (top-level fallback)", targets[0].TeamID)
	}
	// app-b: per-target token/team_id が優先
	if targets[1].Token != "per-b-token" {
		t.Errorf("targets[1].Token = %q, want per-b-token", targets[1].Token)
	}
	if targets[1].TeamID != "per-b-team" {
		t.Errorf("targets[1].TeamID = %q, want per-b-team", targets[1].TeamID)
	}
}

func TestResolveVercelTargets_SelectByName(t *testing.T) {
	// --vercel-project 指定で 1 件のみ返す
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "top-token"
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
		{Name: "app-b", ProjectID: "pid-b"},
	}
	targets, err := cfg.ResolveVercelTargets("app-b")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if targets[0].Name != "app-b" {
		t.Errorf("targets[0].Name = %q, want app-b", targets[0].Name)
	}
	if targets[0].ProjectID != "pid-b" {
		t.Errorf("targets[0].ProjectID = %q, want pid-b", targets[0].ProjectID)
	}
}

func TestResolveVercelTargets_SelectByName_NotFound(t *testing.T) {
	// 一致しない name を指定した場合はエラー
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
	}
	_, err := cfg.ResolveVercelTargets("app-z")
	if err == nil {
		t.Fatal("存在しない name に対してエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "app-z") {
		t.Errorf("エラーメッセージに指定名が含まれることを期待: %v", err)
	}
}

func TestResolveVercelTargets_NoProjects_BackwardCompat(t *testing.T) {
	// projects が未定義の場合は単一解決で 1 件返す（後方互換）
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "env-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "top-token"
	cfg.Vercel.ProjectID = "config-pid"
	targets, err := cfg.ResolveVercelTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1 (後方互換)", len(targets))
	}
	// 環境変数 VERCEL_PROJECT_ID が優先
	if targets[0].ProjectID != "env-pid" {
		t.Errorf("targets[0].ProjectID = %q, want env-pid (env var should win)", targets[0].ProjectID)
	}
	if targets[0].Token != "top-token" {
		t.Errorf("targets[0].Token = %q, want top-token", targets[0].Token)
	}
}

func TestResolveVercelTargets_NoProjects_EmptyProjectID(t *testing.T) {
	// projects 未定義かつ project_id も空の場合は空 ProjectID の 1 件を返す（provider 側でフォールバック）
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "top-token"
	targets, err := cfg.ResolveVercelTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if targets[0].ProjectID != "" {
		t.Errorf("targets[0].ProjectID = %q, want empty (provider 側でフォールバック)", targets[0].ProjectID)
	}
}

func TestResolveVercelTargets_EnvVarToken_Fallback(t *testing.T) {
	// per-target token が空でも VERCEL_TOKEN 環境変数にフォールバック
	t.Setenv("VERCEL_TOKEN", "env-token")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	// top-level token は空（環境変数で取得）
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"}, // per-target token 空
	}
	targets, err := cfg.ResolveVercelTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if targets[0].Token != "env-token" {
		t.Errorf("targets[0].Token = %q, want env-token (VERCEL_TOKEN fallback)", targets[0].Token)
	}
}

func TestResolveGitHubTargets_AllRepos(t *testing.T) {
	// repos が定義されている場合、全件を GitHubTarget として返す
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "top-gh-token"
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"},
		{Name: "backend", Repo: "org/backend", Token: "per-backend-token"},
	}
	targets, err := cfg.ResolveGitHubTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(targets))
	}
	// frontend: top-level token フォールバック
	if targets[0].Token != "top-gh-token" {
		t.Errorf("targets[0].Token = %q, want top-gh-token", targets[0].Token)
	}
	// backend: per-target token が優先
	if targets[1].Token != "per-backend-token" {
		t.Errorf("targets[1].Token = %q, want per-backend-token", targets[1].Token)
	}
}

func TestResolveGitHubTargets_SelectByName(t *testing.T) {
	// --github-repo 指定で 1 件のみ返す
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "top-token"
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"},
		{Name: "backend", Repo: "org/backend"},
	}
	targets, err := cfg.ResolveGitHubTargets("backend")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if targets[0].Name != "backend" {
		t.Errorf("targets[0].Name = %q, want backend", targets[0].Name)
	}
	if targets[0].Repo != "org/backend" {
		t.Errorf("targets[0].Repo = %q, want org/backend", targets[0].Repo)
	}
}

func TestResolveGitHubTargets_SelectByName_NotFound(t *testing.T) {
	// 一致しない name を指定した場合はエラー
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"},
	}
	_, err := cfg.ResolveGitHubTargets("nonexistent")
	if err == nil {
		t.Fatal("存在しない name に対してエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("エラーメッセージに指定名が含まれることを期待: %v", err)
	}
}

func TestResolveGitHubTargets_NoRepos_BackwardCompat(t *testing.T) {
	// repos が未定義の場合は単一解決で 1 件返す（後方互換）
	t.Setenv("GITHUB_TOKEN", "env-gh-token")
	t.Setenv("GITHUB_REPO", "env/repo")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "config-gh-token"
	cfg.GitHub.Repo = "config/repo"
	targets, err := cfg.ResolveGitHubTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1 (後方互換)", len(targets))
	}
	// 環境変数が優先
	if targets[0].Token != "env-gh-token" {
		t.Errorf("targets[0].Token = %q, want env-gh-token", targets[0].Token)
	}
	if targets[0].Repo != "env/repo" {
		t.Errorf("targets[0].Repo = %q, want env/repo", targets[0].Repo)
	}
}

func TestResolveGitHubTargets_NoRepos_EmptyRepo(t *testing.T) {
	// repos 未定義かつ repo も空の場合は空 Repo の 1 件を返す（provider 側でフォールバック）
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "top-token"
	targets, err := cfg.ResolveGitHubTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if targets[0].Repo != "" {
		t.Errorf("targets[0].Repo = %q, want empty (provider 側でフォールバック)", targets[0].Repo)
	}
}

func TestResolveGitHubTargets_EnvVarToken_Fallback(t *testing.T) {
	// per-target token が空でも GITHUB_TOKEN 環境変数にフォールバック
	t.Setenv("GITHUB_TOKEN", "env-gh-token")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"}, // per-target token 空
	}
	targets, err := cfg.ResolveGitHubTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if targets[0].Token != "env-gh-token" {
		t.Errorf("targets[0].Token = %q, want env-gh-token (GITHUB_TOKEN fallback)", targets[0].Token)
	}
}

func TestLoadAppConfig_VercelProjects_EnvRefExpansion(t *testing.T) {
	// per-target token に ${VAR} が書かれている場合、展開される
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-global"))
	chdirCleanup(t, dir)
	t.Setenv("APP_B_TOKEN", "expanded-token")
	if err := os.WriteFile(filepath.Join(dir, ".env-sync.config.yaml"), []byte(`
vercel:
  token: global-token
  projects:
    - name: app-a
      project_id: pid-a
    - name: app-b
      project_id: pid-b
      token: ${APP_B_TOKEN}
`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadAppConfig()
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if cfg.Vercel.Projects[1].Token != "expanded-token" {
		t.Errorf("Projects[1].Token = %q, want expanded-token", cfg.Vercel.Projects[1].Token)
	}
}

// --- Warning 1: selectName が後方互換パスで黙殺されないことの確認 ---

func TestResolveVercelTargets_SelectName_ProjectsEmpty_Error(t *testing.T) {
	// projects が未定義のとき --vercel-project を指定するとエラー
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	// Projects は空（未定義）
	_, err := cfg.ResolveVercelTargets("app-a")
	if err == nil {
		t.Fatal("projects が未定義で --vercel-project 指定時はエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "vercel.projects") {
		t.Errorf("エラーメッセージに vercel.projects が含まれることを期待: %v", err)
	}
}

func TestResolveGitHubTargets_SelectName_ReposEmpty_Error(t *testing.T) {
	// repos が未定義のとき --github-repo を指定するとエラー
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	// Repos は空（未定義）
	_, err := cfg.ResolveGitHubTargets("frontend")
	if err == nil {
		t.Fatal("repos が未定義で --github-repo 指定時はエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "github.repos") {
		t.Errorf("エラーメッセージに github.repos が含まれることを期待: %v", err)
	}
}

// --- Warning 2: name 重複時のエラー ---

func TestResolveVercelTargets_DuplicateName_Error(t *testing.T) {
	// projects に同名 name が複数あるとエラー
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-1"},
		{Name: "app-a", ProjectID: "pid-2"}, // 重複
	}
	_, err := cfg.ResolveVercelTargets("")
	if err == nil {
		t.Fatal("name 重複時にエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "app-a") {
		t.Errorf("エラーメッセージに重複名が含まれることを期待: %v", err)
	}
}

func TestResolveVercelTargets_DuplicateName_WithSelect_Error(t *testing.T) {
	// --vercel-project で指定した場合も重複チェックが行われる
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-1"},
		{Name: "app-a", ProjectID: "pid-2"},
	}
	_, err := cfg.ResolveVercelTargets("app-a")
	if err == nil {
		t.Fatal("name 重複時（selectName 指定）にエラーを期待したが nil")
	}
}

func TestResolveGitHubTargets_DuplicateName_Error(t *testing.T) {
	// repos に同名 name が複数あるとエラー
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend-1"},
		{Name: "frontend", Repo: "org/frontend-2"}, // 重複
	}
	_, err := cfg.ResolveGitHubTargets("")
	if err == nil {
		t.Fatal("name 重複時にエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "frontend") {
		t.Errorf("エラーメッセージに重複名が含まれることを期待: %v", err)
	}
}

// --- Warning 3: name が空のエントリのエラー ---

func TestResolveVercelTargets_EmptyName_Error(t *testing.T) {
	// projects に name が空のエントリがあるとエラー
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "", ProjectID: "pid-1"}, // name 空
	}
	_, err := cfg.ResolveVercelTargets("")
	if err == nil {
		t.Fatal("name が空のエントリでエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("エラーメッセージに name が含まれることを期待: %v", err)
	}
}

func TestResolveGitHubTargets_EmptyName_Error(t *testing.T) {
	// repos に name が空のエントリがあるとエラー
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "", Repo: "org/repo"}, // name 空
	}
	_, err := cfg.ResolveGitHubTargets("")
	if err == nil {
		t.Fatal("name が空のエントリでエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("エラーメッセージに name が含まれることを期待: %v", err)
	}
}

func TestResolveVercelTargets_EmptyProjectID_Error(t *testing.T) {
	// projects に project_id が空のエントリがあるとエラー（絞り込み前に全エントリを検証）
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
		{Name: "app-b", ProjectID: ""}, // project_id 空
	}
	_, err := cfg.ResolveVercelTargets("")
	if err == nil {
		t.Fatal("project_id が空のエントリでエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Errorf("エラーメッセージに project_id が含まれることを期待: %v", err)
	}
}

func TestResolveVercelTargets_EmptyProjectID_SelectName_Error(t *testing.T) {
	// --vercel-project で 1 件に絞り込んでも、project_id 空エントリは絞り込み前に検証される
	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
		{Name: "app-b", ProjectID: ""}, // project_id 空（絞り込み対象外でもエラー）
	}
	_, err := cfg.ResolveVercelTargets("app-a") // app-b を除外しても検証でエラー
	if err == nil {
		t.Fatal("他エントリの project_id が空の場合もエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "project_id") {
		t.Errorf("エラーメッセージに project_id が含まれることを期待: %v", err)
	}
}

func TestResolveGitHubTargets_EmptyRepo_Error(t *testing.T) {
	// repos に repo が空のエントリがあるとエラー（絞り込み前に全エントリを検証）
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"},
		{Name: "backend", Repo: ""}, // repo 空
	}
	_, err := cfg.ResolveGitHubTargets("")
	if err == nil {
		t.Fatal("repo が空のエントリでエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "repo") {
		t.Errorf("エラーメッセージに repo が含まれることを期待: %v", err)
	}
}

func TestResolveGitHubTargets_EmptyRepo_SelectName_Error(t *testing.T) {
	// --github-repo で 1 件に絞り込んでも、repo 空エントリは絞り込み前に検証される
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"},
		{Name: "backend", Repo: ""}, // repo 空（絞り込み対象外でもエラー）
	}
	_, err := cfg.ResolveGitHubTargets("frontend") // backend を除外しても検証でエラー
	if err == nil {
		t.Fatal("他エントリの repo が空の場合もエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "repo") {
		t.Errorf("エラーメッセージに repo が含まれることを期待: %v", err)
	}
}

// --- 取得元(source)付きリゾルバのテスト ---

func TestResolveVercelTokenWithSource_Env(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "env-tok")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "cfg-tok"
	val, src := cfg.ResolveVercelTokenWithSource()
	if val != "env-tok" {
		t.Errorf("val = %q, want env-tok", val)
	}
	if src != "env" {
		t.Errorf("src = %q, want env", src)
	}
}

func TestResolveVercelTokenWithSource_Config(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Token = "cfg-tok"
	val, src := cfg.ResolveVercelTokenWithSource()
	if val != "cfg-tok" {
		t.Errorf("val = %q, want cfg-tok", val)
	}
	if src != "config" {
		t.Errorf("src = %q, want config", src)
	}
}

func TestResolveVercelTokenWithSource_Unset(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "")
	cfg := &config.AppConfig{}
	val, src := cfg.ResolveVercelTokenWithSource()
	if val != "" {
		t.Errorf("val = %q, want empty", val)
	}
	if src != "" {
		t.Errorf("src = %q, want empty", src)
	}
}

func TestResolveVercelProjectIDWithSource_Env(t *testing.T) {
	t.Setenv("VERCEL_PROJECT_ID", "env-pid")
	cfg := &config.AppConfig{}
	cfg.Vercel.ProjectID = "cfg-pid"
	val, src := cfg.ResolveVercelProjectIDWithSource()
	if val != "env-pid" {
		t.Errorf("val = %q, want env-pid", val)
	}
	if src != "env" {
		t.Errorf("src = %q, want env", src)
	}
}

func TestResolveVercelProjectIDWithSource_Config(t *testing.T) {
	t.Setenv("VERCEL_PROJECT_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.ProjectID = "cfg-pid"
	val, src := cfg.ResolveVercelProjectIDWithSource()
	if val != "cfg-pid" {
		t.Errorf("val = %q, want cfg-pid", val)
	}
	if src != "config" {
		t.Errorf("src = %q, want config", src)
	}
}

func TestResolveVercelTeamIDWithSource_Env(t *testing.T) {
	t.Setenv("VERCEL_TEAM_ID", "env-team")
	cfg := &config.AppConfig{}
	cfg.Vercel.TeamID = "cfg-team"
	val, src := cfg.ResolveVercelTeamIDWithSource()
	if val != "env-team" {
		t.Errorf("val = %q, want env-team", val)
	}
	if src != "env" {
		t.Errorf("src = %q, want env", src)
	}
}

func TestResolveGitHubTokenWithSource_Env(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-gh-tok")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "cfg-gh-tok"
	val, src := cfg.ResolveGitHubTokenWithSource()
	if val != "env-gh-tok" {
		t.Errorf("val = %q, want env-gh-tok", val)
	}
	if src != "env" {
		t.Errorf("src = %q, want env", src)
	}
}

func TestResolveGitHubTokenWithSource_Config(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Token = "cfg-gh-tok"
	val, src := cfg.ResolveGitHubTokenWithSource()
	if val != "cfg-gh-tok" {
		t.Errorf("val = %q, want cfg-gh-tok", val)
	}
	if src != "config" {
		t.Errorf("src = %q, want config", src)
	}
}

func TestResolveGitHubRepoWithSource_Env(t *testing.T) {
	t.Setenv("GITHUB_REPO", "env/repo")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repo = "cfg/repo"
	val, src := cfg.ResolveGitHubRepoWithSource()
	if val != "env/repo" {
		t.Errorf("val = %q, want env/repo", val)
	}
	if src != "env" {
		t.Errorf("src = %q, want env", src)
	}
}

func TestResolveGitHubRepoWithSource_Config(t *testing.T) {
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repo = "cfg/repo"
	val, src := cfg.ResolveGitHubRepoWithSource()
	if val != "cfg/repo" {
		t.Errorf("val = %q, want cfg/repo", val)
	}
	if src != "config" {
		t.Errorf("src = %q, want config", src)
	}
}

func TestResolveVercelTargets_SourceFields_SingleTarget(t *testing.T) {
	// 単一ターゲット（projects 未定義）の source フィールドが設定されることを確認する
	t.Setenv("VERCEL_TOKEN", "env-tok")
	t.Setenv("VERCEL_PROJECT_ID", "env-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.TeamID = "cfg-team"
	targets, err := cfg.ResolveVercelTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if targets[0].TokenSource != "env" {
		t.Errorf("TokenSource = %q, want env", targets[0].TokenSource)
	}
	if targets[0].ProjectIDSource != "env" {
		t.Errorf("ProjectIDSource = %q, want env", targets[0].ProjectIDSource)
	}
	if targets[0].TeamIDSource != "config" {
		t.Errorf("TeamIDSource = %q, want config", targets[0].TeamIDSource)
	}
}

func TestResolveVercelTargets_SourceFields_MultiTarget(t *testing.T) {
	// 複数ターゲット（projects 定義あり）の source フィールドが設定されることを確認する
	t.Setenv("VERCEL_TOKEN", "env-tok")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")
	cfg := &config.AppConfig{}
	cfg.Vercel.Projects = []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},                     // per-target token なし → env fallback
		{Name: "app-b", ProjectID: "pid-b", Token: "per-b-tok"}, // per-target token あり
	}
	targets, err := cfg.ResolveVercelTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(targets))
	}
	// app-a: VERCEL_TOKEN (env) からフォールバック
	if targets[0].TokenSource != "env" {
		t.Errorf("targets[0].TokenSource = %q, want env", targets[0].TokenSource)
	}
	if targets[0].ProjectIDSource != "config" {
		t.Errorf("targets[0].ProjectIDSource = %q, want config", targets[0].ProjectIDSource)
	}
	// app-b: per-target token は config 由来
	if targets[1].TokenSource != "config" {
		t.Errorf("targets[1].TokenSource = %q, want config", targets[1].TokenSource)
	}
}

func TestResolveGitHubTargets_SourceFields_SingleTarget(t *testing.T) {
	// 単一ターゲット（repos 未定義）の source フィールドが設定されることを確認する
	t.Setenv("GITHUB_TOKEN", "env-gh-tok")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repo = "cfg/repo"
	targets, err := cfg.ResolveGitHubTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(targets))
	}
	if targets[0].TokenSource != "env" {
		t.Errorf("TokenSource = %q, want env", targets[0].TokenSource)
	}
	if targets[0].RepoSource != "config" {
		t.Errorf("RepoSource = %q, want config", targets[0].RepoSource)
	}
}

func TestResolveGitHubTargets_SourceFields_MultiTarget(t *testing.T) {
	// 複数ターゲット（repos 定義あり）の source フィールドが設定されることを確認する
	t.Setenv("GITHUB_TOKEN", "env-gh-tok")
	t.Setenv("GITHUB_REPO", "")
	cfg := &config.AppConfig{}
	cfg.GitHub.Repos = []config.GitHubRepoConf{
		{Name: "frontend", Repo: "org/frontend"},
		{Name: "backend", Repo: "org/backend", Token: "per-backend-tok"},
	}
	targets, err := cfg.ResolveGitHubTargets("")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(targets))
	}
	// frontend: GITHUB_TOKEN (env) からフォールバック
	if targets[0].TokenSource != "env" {
		t.Errorf("targets[0].TokenSource = %q, want env", targets[0].TokenSource)
	}
	if targets[0].RepoSource != "config" {
		t.Errorf("targets[0].RepoSource = %q, want config", targets[0].RepoSource)
	}
	// backend: per-target token は config 由来
	if targets[1].TokenSource != "config" {
		t.Errorf("targets[1].TokenSource = %q, want config", targets[1].TokenSource)
	}
}
