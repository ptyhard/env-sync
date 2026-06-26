package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ptyhard/env-sync/internal/config"
	_ "github.com/ptyhard/env-sync/internal/provider/github"
	_ "github.com/ptyhard/env-sync/internal/provider/vercel"
)

// --- BuildInitYAML のテスト ---

func TestBuildInitYAML_ContainsDefaults(t *testing.T) {
	out := config.BuildInitYAML([]string{"FOO", "BAR"})
	if !strings.Contains(out, "defaults:") {
		t.Error("defaults: セクションが含まれていない")
	}
	if !strings.Contains(out, "secret: true") {
		t.Error("defaults の secret: true が含まれていない")
	}
}

func TestBuildInitYAML_NextPublicIsSecretFalse(t *testing.T) {
	out := config.BuildInitYAML([]string{"NEXT_PUBLIC_API_URL", "NEXT_PUBLIC_SITE"})
	if !strings.Contains(out, "NEXT_PUBLIC_API_URL: { secret: false }") {
		t.Errorf("NEXT_PUBLIC_API_URL が secret: false になっていない:\n%s", out)
	}
	if !strings.Contains(out, "NEXT_PUBLIC_SITE: { secret: false }") {
		t.Errorf("NEXT_PUBLIC_SITE が secret: false になっていない:\n%s", out)
	}
}

func TestBuildInitYAML_OtherKeyIsSecretTrue(t *testing.T) {
	out := config.BuildInitYAML([]string{"DATABASE_URL", "SECRET_KEY"})
	if !strings.Contains(out, "DATABASE_URL: { secret: true }") {
		t.Errorf("DATABASE_URL が secret: true になっていない:\n%s", out)
	}
	if !strings.Contains(out, "SECRET_KEY: { secret: true }") {
		t.Errorf("SECRET_KEY が secret: true になっていない:\n%s", out)
	}
}

func TestBuildInitYAML_MixedKeys(t *testing.T) {
	out := config.BuildInitYAML([]string{"DATABASE_URL", "NEXT_PUBLIC_API_BASE_URL", "DEBUG"})
	if !strings.Contains(out, "NEXT_PUBLIC_API_BASE_URL: { secret: false }") {
		t.Errorf("NEXT_PUBLIC_ プレフィックスが secret: false にならない:\n%s", out)
	}
	if !strings.Contains(out, "DATABASE_URL: { secret: true }") {
		t.Errorf("DATABASE_URL が secret: true にならない:\n%s", out)
	}
	if !strings.Contains(out, "DEBUG: { secret: true }") {
		t.Errorf("DEBUG が secret: true にならない:\n%s", out)
	}
}

func TestBuildInitYAML_NoValuesIncluded(t *testing.T) {
	out := config.BuildInitYAML([]string{"API_KEY", "DATABASE_URL"})
	if strings.Contains(out, "postgres://") || strings.Contains(out, "https://") {
		t.Error("yaml に値が含まれている")
	}
}

func TestBuildInitYAML_ZeroKeys(t *testing.T) {
	out := config.BuildInitYAML([]string{})
	if !strings.Contains(out, "variables:") {
		t.Error("variables: セクションが含まれていない")
	}
	if !strings.Contains(out, "#") {
		t.Error("0件の場合に例コメントが含まれていない")
	}
}

func TestBuildInitYAML_ContainsVariablesSection(t *testing.T) {
	out := config.BuildInitYAML([]string{"FOO"})
	if !strings.Contains(out, "variables:") {
		t.Error("variables: セクションが含まれていない")
	}
}

func TestBuildInitYAML_ContainsWarningComment(t *testing.T) {
	out := config.BuildInitYAML([]string{"FOO"})
	if !strings.Contains(out, "雛形") || !strings.Contains(out, "見直す") {
		t.Errorf("注意コメントが含まれていない:\n%s", out)
	}
}

// --- ParseInitFlags のテスト ---

func TestParseInitFlags_Defaults(t *testing.T) {
	opts := config.ParseInitFlags([]string{}, nil)
	if opts.Env != ".env" {
		t.Errorf("Env のデフォルト値が異なる: got %q", opts.Env)
	}
	if opts.Def != "env-sync.yaml" {
		t.Errorf("Def のデフォルト値が異なる: got %q", opts.Def)
	}
	if opts.Force {
		t.Error("Force のデフォルト値が true になっている")
	}
}

func TestParseInitFlags_EnvFlag(t *testing.T) {
	opts := config.ParseInitFlags([]string{"--env", ".env.production"}, nil)
	if opts.Env != ".env.production" {
		t.Errorf("--env の値が反映されない: got %q", opts.Env)
	}
}

func TestParseInitFlags_EnvEqualFlag(t *testing.T) {
	opts := config.ParseInitFlags([]string{"--env=.env.production"}, nil)
	if opts.Env != ".env.production" {
		t.Errorf("--env= の値が反映されない: got %q", opts.Env)
	}
}

func TestParseInitFlags_DefFlag(t *testing.T) {
	opts := config.ParseInitFlags([]string{"--def", "custom.yaml"}, nil)
	if opts.Def != "custom.yaml" {
		t.Errorf("--def の値が反映されない: got %q", opts.Def)
	}
}

func TestParseInitFlags_DefEqualFlag(t *testing.T) {
	opts := config.ParseInitFlags([]string{"--def=custom.yaml"}, nil)
	if opts.Def != "custom.yaml" {
		t.Errorf("--def= の値が反映されない: got %q", opts.Def)
	}
}

func TestParseInitFlags_ForceFlag(t *testing.T) {
	opts := config.ParseInitFlags([]string{"--force"}, nil)
	if !opts.Force {
		t.Error("--force が有効にならない")
	}
}

func TestParseInitFlags_MultipleFlags(t *testing.T) {
	opts := config.ParseInitFlags([]string{"--env", ".env.staging", "--def=out.yaml", "--force"}, nil)
	if opts.Env != ".env.staging" {
		t.Errorf("Env が間違っている: got %q", opts.Env)
	}
	if opts.Def != "out.yaml" {
		t.Errorf("Def が間違っている: got %q", opts.Def)
	}
	if !opts.Force {
		t.Error("Force が有効にならない")
	}
}

// --- RunInit の統合テスト（一時ディレクトリを利用）---

func TestRunInit_CreatesYAML(t *testing.T) {
	dir := t.TempDir()

	envFile := filepath.Join(dir, ".env")
	defFile := filepath.Join(dir, "env-sync.yaml")

	if err := os.WriteFile(envFile, []byte("DATABASE_URL=postgres://x\nNEXT_PUBLIC_API=https://y\nDEBUG=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := config.RunInit([]string{"--env", envFile, "--def", defFile}, nil)
	if err != nil {
		t.Fatalf("RunInit でエラー: %v", err)
	}

	data, err := os.ReadFile(defFile)
	if err != nil {
		t.Fatalf("出力ファイルが存在しない: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "DATABASE_URL") {
		t.Error("DATABASE_URL が出力に含まれていない")
	}
	if !strings.Contains(content, "NEXT_PUBLIC_API") {
		t.Error("NEXT_PUBLIC_API が出力に含まれていない")
	}
	// 値が含まれないこと
	if strings.Contains(content, "postgres://x") {
		t.Error("値 postgres://x が出力に含まれている（NG）")
	}
	if strings.Contains(content, "https://y") {
		t.Error("値 https://y が出力に含まれている（NG）")
	}
}

func TestRunInit_OverwriteProtection(t *testing.T) {
	dir := t.TempDir()

	envFile := filepath.Join(dir, ".env")
	defFile := filepath.Join(dir, "env-sync.yaml")

	if err := os.WriteFile(envFile, []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defFile, []byte("existing content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := config.RunInit([]string{"--env", envFile, "--def", defFile}, nil)
	if err == nil {
		t.Fatal("--force なしで既存ファイルが上書きされてしまった（エラーが返らなかった）")
	}
	if !strings.Contains(err.Error(), "上書きするには --force") {
		t.Errorf("エラーメッセージが想定と異なる: %v", err)
	}

	data, _ := os.ReadFile(defFile)
	if string(data) != "existing content" {
		t.Error("上書き保護が機能していない: ファイルの中身が変わっている")
	}
}

func TestRunInit_ForceOverwrite(t *testing.T) {
	dir := t.TempDir()

	envFile := filepath.Join(dir, ".env")
	defFile := filepath.Join(dir, "env-sync.yaml")

	if err := os.WriteFile(envFile, []byte("FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(defFile, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := config.RunInit([]string{"--env", envFile, "--def", defFile, "--force"}, nil)
	if err != nil {
		t.Fatalf("--force 付きで RunInit がエラー: %v", err)
	}

	data, err := os.ReadFile(defFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old content" {
		t.Error("--force 付きでも上書きされていない")
	}
}

func TestRunInit_EnvFileNotFound(t *testing.T) {
	dir := t.TempDir()
	err := config.RunInit([]string{"--env", filepath.Join(dir, "nonexistent.env")}, nil)
	if err == nil {
		t.Error("存在しない env ファイルでエラーが返らなかった")
	}
}

func TestRunInit_CommentsNotValues(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	defFile := filepath.Join(dir, "out.yaml")

	if err := os.WriteFile(envFile, []byte("# これはコメント\nSECRET_KEY=supersecret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := config.RunInit([]string{"--env", envFile, "--def", defFile}, nil); err != nil {
		t.Fatalf("RunInit エラー: %v", err)
	}

	data, _ := os.ReadFile(defFile)
	content := string(data)

	if strings.Contains(content, "supersecret") {
		t.Error("値 supersecret が yaml に混入している（NG）")
	}
	if !strings.Contains(content, "SECRET_KEY") {
		t.Error("SECRET_KEY がキーとして含まれていない")
	}
}

// --- 往復（round-trip）統合テスト ---
// init が生成した YAML が Definition 構造体へそのまま読み込め、
// secret / environments が想定どおり解決されることを保証する。

func TestInitYAML_RoundTripParsesAsDefinition(t *testing.T) {
	keys := []string{"DATABASE_URL", "DEBUG", "NEXT_PUBLIC_API"}
	out := config.BuildInitYAML(keys)

	var def config.Definition
	if err := yaml.Unmarshal([]byte(out), &def); err != nil {
		t.Fatalf("生成 YAML が既存パーサで読めない: %v", err)
	}

	// defaults.secret が true であること
	if def.Defaults.Secret == nil || !*def.Defaults.Secret {
		t.Errorf("defaults.secret = nil or false, want true")
	}

	// 全キーが variables に列挙されているか
	for _, k := range keys {
		if _, ok := def.Variables[k]; !ok {
			t.Errorf("variables に %s が含まれていない", k)
		}
	}

	// secret サジェスト: NEXT_PUBLIC_ → false、他 → true
	cases := map[string]bool{
		"NEXT_PUBLIC_API": false,
		"DATABASE_URL":    true,
		"DEBUG":           true,
	}
	for key, wantSecret := range cases {
		conf := def.Variables[key]
		if conf.Secret == nil {
			t.Errorf("%s の secret が nil", key)
			continue
		}
		if *conf.Secret != wantSecret {
			t.Errorf("%s の secret = %v, want %v", key, *conf.Secret, wantSecret)
		}
	}
}

// init 生成 → SortedKeys まで通ることを確認する。
func TestInitYAML_RoundTripResolvesEntries(t *testing.T) {
	out := config.BuildInitYAML([]string{"NEXT_PUBLIC_FOO", "BAR_SECRET"})

	var def config.Definition
	if err := yaml.Unmarshal([]byte(out), &def); err != nil {
		t.Fatalf("生成 YAML のパースに失敗: %v", err)
	}

	defKeys := config.SortedKeys(def.Variables)
	if len(defKeys) != 2 {
		t.Fatalf("defKeys len = %d, want 2", len(defKeys))
	}

	cases := map[string]bool{
		"NEXT_PUBLIC_FOO": false,
		"BAR_SECRET":      true,
	}
	for key, wantSecret := range cases {
		conf := def.Variables[key]
		if conf.Secret == nil {
			t.Errorf("%s の secret が nil", key)
			continue
		}
		if *conf.Secret != wantSecret {
			t.Errorf("%s の secret = %v, want %v", key, *conf.Secret, wantSecret)
		}
	}
}
