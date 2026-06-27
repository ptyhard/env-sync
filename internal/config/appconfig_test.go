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
