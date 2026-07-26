package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ptyhard/env-sync/internal/config"
)

// --- BuildSetupYAML のテスト ---

func TestBuildSetupYAML_VercelOnly(t *testing.T) {
	answers := config.SetupAnswers{
		UseVercel:       true,
		VercelProjectID: "prj_abc123",
		VercelTeamID:    "team_xyz",
		VercelTokenRef:  "${VERCEL_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if !strings.Contains(out, "vercel:") {
		t.Error("vercel: セクションが含まれていない")
	}
	if !strings.Contains(out, "prj_abc123") {
		t.Errorf("project_id が含まれていない:\n%s", out)
	}
	if !strings.Contains(out, "team_xyz") {
		t.Errorf("team_id が含まれていない:\n%s", out)
	}
	if strings.Contains(out, "github:") {
		t.Error("github: セクションが含まれているが UseGitHub=false のはず")
	}
}

func TestBuildSetupYAML_GitHubOnly(t *testing.T) {
	answers := config.SetupAnswers{
		UseGitHub:      true,
		GitHubRepo:     "myorg/myrepo",
		GitHubTokenRef: "${GITHUB_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if !strings.Contains(out, "github:") {
		t.Error("github: セクションが含まれていない")
	}
	if !strings.Contains(out, "myorg/myrepo") {
		t.Errorf("repo が含まれていない:\n%s", out)
	}
	if strings.Contains(out, "vercel:") {
		t.Error("vercel: セクションが含まれているが UseVercel=false のはず")
	}
}

func TestBuildSetupYAML_BothProviders(t *testing.T) {
	answers := config.SetupAnswers{
		UseVercel:       true,
		VercelProjectID: "prj_xxx",
		VercelTokenRef:  "${VERCEL_TOKEN}",
		UseGitHub:       true,
		GitHubRepo:      "owner/repo",
		GitHubTokenRef:  "${GITHUB_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if !strings.Contains(out, "vercel:") {
		t.Error("vercel: セクションが含まれていない")
	}
	if !strings.Contains(out, "github:") {
		t.Error("github: セクションが含まれていない")
	}
}

func TestBuildSetupYAML_TokenEnvRef(t *testing.T) {
	answers := config.SetupAnswers{
		UseVercel:      true,
		VercelTokenRef: "${VERCEL_TOKEN}",
		UseGitHub:      true,
		GitHubTokenRef: "${GITHUB_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if !strings.Contains(out, "${VERCEL_TOKEN}") {
		t.Errorf("Vercel token が環境変数参照形式になっていない:\n%s", out)
	}
	if !strings.Contains(out, "${GITHUB_TOKEN}") {
		t.Errorf("GitHub token が環境変数参照形式になっていない:\n%s", out)
	}
}

func TestBuildSetupYAML_TokenPlain(t *testing.T) {
	answers := config.SetupAnswers{
		UseVercel:      true,
		VercelTokenRef: "my-plain-vercel-token",
		HasPlainToken:  true,
		UseGitHub:      false,
	}
	out := config.BuildSetupYAML(answers)
	if !strings.Contains(out, "my-plain-vercel-token") {
		t.Errorf("平文トークンが含まれていない:\n%s", out)
	}
	if strings.Contains(out, "${VERCEL_TOKEN}") {
		t.Errorf("平文指定なのに env ref 形式になっている:\n%s", out)
	}
}

func TestBuildSetupYAML_NoTeamID(t *testing.T) {
	answers := config.SetupAnswers{
		UseVercel:       true,
		VercelProjectID: "prj_xxx",
		VercelTeamID:    "", // 未入力
		VercelTokenRef:  "${VERCEL_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if strings.Contains(out, "team_id:") {
		t.Errorf("team_id が空なのに YAML に含まれている:\n%s", out)
	}
}

func TestBuildSetupYAML_NeitherProvider(t *testing.T) {
	answers := config.SetupAnswers{
		UseVercel: false,
		UseGitHub: false,
	}
	out := config.BuildSetupYAML(answers)
	if strings.Contains(out, "vercel:") {
		t.Error("vercel: が含まれているが UseVercel=false のはず")
	}
	if strings.Contains(out, "github:") {
		t.Error("github: が含まれているが UseGitHub=false のはず")
	}
}

// --- YAML 生成 → AppConfig ラウンドトリップ ---

func TestBuildSetupYAML_RoundTripAsAppConfig(t *testing.T) {
	t.Setenv("VERCEL_TOKEN", "vt-test")
	t.Setenv("GITHUB_TOKEN", "gh-test")

	answers := config.SetupAnswers{
		UseVercel:       true,
		VercelProjectID: "prj_round",
		VercelTeamID:    "team_round",
		VercelTokenRef:  "${VERCEL_TOKEN}",
		UseGitHub:       true,
		GitHubRepo:      "org/repo",
		GitHubTokenRef:  "${GITHUB_TOKEN}",
	}
	yamlText := config.BuildSetupYAML(answers)

	// AppConfig スキーマとして YAML パースできること
	var cfg config.AppConfig
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		t.Fatalf("生成 YAML が AppConfig としてパースできない: %v\n%s", err, yamlText)
	}
	if cfg.Vercel.ProjectID != "prj_round" {
		t.Errorf("Vercel.ProjectID = %q, want prj_round", cfg.Vercel.ProjectID)
	}
	if cfg.Vercel.TeamID != "team_round" {
		t.Errorf("Vercel.TeamID = %q, want team_round", cfg.Vercel.TeamID)
	}
	if cfg.Vercel.Token != "${VERCEL_TOKEN}" {
		t.Errorf("Vercel.Token = %q, want ${VERCEL_TOKEN}", cfg.Vercel.Token)
	}
	if cfg.GitHub.Repo != "org/repo" {
		t.Errorf("GitHub.Repo = %q, want org/repo", cfg.GitHub.Repo)
	}
}

// --- WriteSetupFile のテスト ---

func TestWriteSetupFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := config.WriteSetupFile(path, "content: test\n", 0o644, false); err != nil {
		t.Fatalf("WriteSetupFile エラー: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ファイルが作成されていない: %v", err)
	}
	if string(data) != "content: test\n" {
		t.Errorf("ファイル内容が異なる: %q", string(data))
	}
}

func TestWriteSetupFile_OverwriteProtection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := config.WriteSetupFile(path, "new content", 0o644, false)
	if err == nil {
		t.Fatal("--force なしで既存ファイルへの上書きが成功してしまった")
	}
	// "--force" は en/ja 両カタログのファイル存在エラーに共通して含まれる
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("エラーメッセージが想定と異なる: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "existing" {
		t.Error("上書き保護が機能していない: ファイルの中身が変わっている")
	}
}

func TestWriteSetupFile_ForceOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := config.WriteSetupFile(path, "new", 0o644, true); err != nil {
		t.Fatalf("--force 付きで WriteSetupFile がエラー: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Error("--force 付きでも上書きされていない")
	}
}

func TestWriteSetupFile_ForceOverwrite_Permissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.yaml")

	// 既存ファイルを 0644 で作成しておく
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	// --force で 0600 を指定して上書き
	if err := config.WriteSetupFile(path, "new", 0o600, true); err != nil {
		t.Fatalf("--force + 0600 で WriteSetupFile がエラー: %v", err)
	}

	// 上書き後にパーミッションが 0600 に変更されていること
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("--force 上書き後のパーミッションが 0600 でない: %04o", info.Mode().Perm())
	}
	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Error("--force 付きでも内容が上書きされていない")
	}
}

func TestWriteSetupFile_Permissions_0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.yaml")

	if err := config.WriteSetupFile(path, "token: x\n", 0o600, false); err != nil {
		t.Fatalf("WriteSetupFile エラー: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("パーミッションが 0600 でない: %04o", info.Mode().Perm())
	}
}

func TestWriteSetupFile_Permissions_0644(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := config.WriteSetupFile(path, "content\n", 0o644, false); err != nil {
		t.Fatalf("WriteSetupFile エラー: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("パーミッションが 0644 でない: %04o", info.Mode().Perm())
	}
}

// --- RunSetupWithReader の統合テスト ---

// simulateInput は対話入力をシミュレートする文字列 Reader を返す。
func simulateInput(answers ...string) *strings.Reader {
	return strings.NewReader(strings.Join(answers, "\n") + "\n")
}

func TestRunSetupWithReader_ProjectConfig_EnvRef(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	// Vercel yes, GitHub yes, 両方環境変数参照
	in := simulateInput("Y", "prj_test", "", "Y", "Y", "owner/repo", "Y")
	if err := config.RunSetupWithReader([]string{}, nil, in); err != nil {
		t.Fatalf("RunSetupWithReader エラー: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env-sync.config.yaml"))
	if err != nil {
		t.Fatalf(".env-sync.config.yaml が作成されていない: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "prj_test") {
		t.Errorf("project_id が含まれていない:\n%s", content)
	}
	if !strings.Contains(content, "${VERCEL_TOKEN}") {
		t.Errorf("Vercel token が環境変数参照になっていない:\n%s", content)
	}
	if !strings.Contains(content, "owner/repo") {
		t.Errorf("GitHub repo が含まれていない:\n%s", content)
	}
	if !strings.Contains(content, "${GITHUB_TOKEN}") {
		t.Errorf("GitHub token が環境変数参照になっていない:\n%s", content)
	}
}

func TestRunSetupWithReader_ProjectConfig_OverwriteProtection(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	existing := filepath.Join(dir, ".env-sync.config.yaml")
	if err := os.WriteFile(existing, []byte("existing: true"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := simulateInput("Y", "prj_test", "", "Y", "n")
	err := config.RunSetupWithReader([]string{}, nil, in)
	if err == nil {
		t.Fatal("--force なしで既存ファイルへの上書きが成功してしまった")
	}
	// "--force" は en/ja 両カタログのファイル存在エラーに共通して含まれる
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("エラーメッセージが想定と異なる: %v", err)
	}
}

func TestRunSetupWithReader_ProjectConfig_Force(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	existing := filepath.Join(dir, ".env-sync.config.yaml")
	if err := os.WriteFile(existing, []byte("old: true"), 0o644); err != nil {
		t.Fatal(err)
	}

	in := simulateInput("Y", "prj_new", "", "Y", "n")
	if err := config.RunSetupWithReader([]string{"--force"}, nil, in); err != nil {
		t.Fatalf("--force 付きで RunSetupWithReader がエラー: %v", err)
	}

	data, _ := os.ReadFile(existing)
	if !strings.Contains(string(data), "prj_new") {
		t.Errorf("--force 付きでも上書きされていない: %s", string(data))
	}
}

func TestRunSetupWithReader_GlobalConfig(t *testing.T) {
	globalDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", globalDir)

	// Vercel のみ設定
	in := simulateInput("Y", "prj_global", "", "Y", "n")
	if err := config.RunSetupWithReader([]string{"--global"}, nil, in); err != nil {
		t.Fatalf("--global で RunSetupWithReader エラー: %v", err)
	}

	expectedPath := filepath.Join(globalDir, "env-sync", "config.yaml")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("global config が作成されていない (%s): %v", expectedPath, err)
	}

	content := string(data)
	if !strings.Contains(content, "prj_global") {
		t.Errorf("project_id が含まれていない:\n%s", content)
	}

	// --global では 0600 であること
	info, err := os.Stat(expectedPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("--global 出力のパーミッションが 0600 でない: %04o", info.Mode().Perm())
	}
}

func TestRunSetupWithReader_PlainToken_0600(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	// Vercel yes, 平文トークン指定, GitHub no
	in := simulateInput("Y", "prj_x", "", "n", "my-secret-token", "n")
	if err := config.RunSetupWithReader([]string{}, nil, in); err != nil {
		t.Fatalf("RunSetupWithReader エラー: %v", err)
	}

	path := filepath.Join(dir, ".env-sync.config.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("平文トークン指定時のパーミッションが 0600 でない: %04o", info.Mode().Perm())
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "my-secret-token") {
		t.Errorf("平文トークンが含まれていない:\n%s", string(data))
	}
}

func TestRunSetupWithReader_NeitherProvider(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	in := simulateInput("n", "n", "n")
	if err := config.RunSetupWithReader([]string{}, nil, in); err != nil {
		t.Fatalf("RunSetupWithReader エラー: %v", err)
	}

	// ファイルは作成されるが vercel/github/cloudflare セクションは含まれない
	data, err := os.ReadFile(filepath.Join(dir, ".env-sync.config.yaml"))
	if err != nil {
		t.Fatalf(".env-sync.config.yaml が作成されていない: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "vercel:") {
		t.Error("vercel: セクションが含まれているがすべて n を選択した")
	}
	if strings.Contains(content, "github:") {
		t.Error("github: セクションが含まれているがすべて n を選択した")
	}
	if strings.Contains(content, "cloudflare:") {
		t.Error("cloudflare: セクションが含まれているがすべて n を選択した")
	}
}

// --- ParseSetupFlags のテスト ---

func TestParseSetupFlags_Defaults(t *testing.T) {
	opts := config.ParseSetupFlags([]string{}, nil)
	if opts.Global {
		t.Error("Global のデフォルト値が true になっている")
	}
	if opts.Force {
		t.Error("Force のデフォルト値が true になっている")
	}
}

func TestParseSetupFlags_GlobalFlag(t *testing.T) {
	opts := config.ParseSetupFlags([]string{"--global"}, nil)
	if !opts.Global {
		t.Error("--global が有効にならない")
	}
}

func TestParseSetupFlags_ForceFlag(t *testing.T) {
	opts := config.ParseSetupFlags([]string{"--force"}, nil)
	if !opts.Force {
		t.Error("--force が有効にならない")
	}
}

func TestParseSetupFlags_Both(t *testing.T) {
	opts := config.ParseSetupFlags([]string{"--global", "--force"}, nil)
	if !opts.Global || !opts.Force {
		t.Errorf("--global --force が反映されない: %+v", opts)
	}
}

// --- Cloudflare の setup テスト ---

func TestBuildSetupYAML_CloudflareOnly(t *testing.T) {
	answers := config.SetupAnswers{
		UseCloudflare:       true,
		CloudflareAccountID: "acct_abc",
		CloudflareScript:    "my-worker",
		CloudflareTokenRef:  "${CLOUDFLARE_API_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if !strings.Contains(out, "cloudflare:") {
		t.Error("cloudflare: セクションが含まれていない")
	}
	if !strings.Contains(out, "acct_abc") {
		t.Errorf("account_id が含まれていない:\n%s", out)
	}
	if !strings.Contains(out, "my-worker") {
		t.Errorf("script が含まれていない:\n%s", out)
	}
	if strings.Contains(out, "vercel:") || strings.Contains(out, "github:") {
		t.Error("vercel: / github: セクションが含まれているが選択していない")
	}
}

func TestBuildSetupYAML_CloudflareOmitsEmptyScript(t *testing.T) {
	// script 未入力（wrangler 設定から解決する運用）のときは script 行を出さない
	answers := config.SetupAnswers{
		UseCloudflare:       true,
		CloudflareAccountID: "acct_abc",
		CloudflareTokenRef:  "${CLOUDFLARE_API_TOKEN}",
	}
	out := config.BuildSetupYAML(answers)
	if strings.Contains(out, "script:") {
		t.Errorf("script が空なのに script: 行が出力されている:\n%s", out)
	}
}

func TestBuildSetupYAML_CloudflareRoundTripAsAppConfig(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "cf-test")

	answers := config.SetupAnswers{
		UseCloudflare:       true,
		CloudflareAccountID: "acct_round",
		CloudflareScript:    "round-worker",
		CloudflareTokenRef:  "${CLOUDFLARE_API_TOKEN}",
	}
	yamlText := config.BuildSetupYAML(answers)

	var cfg config.AppConfig
	if err := yaml.Unmarshal([]byte(yamlText), &cfg); err != nil {
		t.Fatalf("生成 YAML が AppConfig としてパースできない: %v\n%s", err, yamlText)
	}
	if cfg.Cloudflare.AccountID != "acct_round" {
		t.Errorf("Cloudflare.AccountID = %q, want acct_round", cfg.Cloudflare.AccountID)
	}
	if cfg.Cloudflare.Script != "round-worker" {
		t.Errorf("Cloudflare.Script = %q, want round-worker", cfg.Cloudflare.Script)
	}
	if cfg.Cloudflare.APIToken != "${CLOUDFLARE_API_TOKEN}" {
		t.Errorf("Cloudflare.APIToken = %q, want ${CLOUDFLARE_API_TOKEN}", cfg.Cloudflare.APIToken)
	}
}

func TestRunSetupWithReader_CloudflareEnvRef(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	// Vercel n, GitHub n, Cloudflare yes（account_id / script / 環境変数参照）
	in := simulateInput("n", "n", "Y", "acct_cli", "cli-worker", "Y")
	if err := config.RunSetupWithReader([]string{}, nil, in); err != nil {
		t.Fatalf("RunSetupWithReader エラー: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".env-sync.config.yaml"))
	if err != nil {
		t.Fatalf(".env-sync.config.yaml が作成されていない: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "cloudflare:") {
		t.Errorf("cloudflare: セクションが含まれていない:\n%s", content)
	}
	if !strings.Contains(content, "acct_cli") {
		t.Errorf("account_id が含まれていない:\n%s", content)
	}
	if !strings.Contains(content, "cli-worker") {
		t.Errorf("script が含まれていない:\n%s", content)
	}
	if !strings.Contains(content, "${CLOUDFLARE_API_TOKEN}") {
		t.Errorf("Cloudflare token が環境変数参照になっていない:\n%s", content)
	}
}

func TestRunSetupWithReader_CloudflarePlainToken_0600(t *testing.T) {
	dir := t.TempDir()
	chdirCleanup(t, dir)

	// Cloudflare のみ・平文トークン入力 → 0600 で作成される
	in := simulateInput("n", "n", "Y", "acct_cli", "", "n", "cf-raw-token")
	if err := config.RunSetupWithReader([]string{}, nil, in); err != nil {
		t.Fatalf("RunSetupWithReader エラー: %v", err)
	}

	path := filepath.Join(dir, ".env-sync.config.yaml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("ファイルが作成されていない: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("パーミッション = %04o, want 0600（平文トークンを含むため）", info.Mode().Perm())
	}
}
