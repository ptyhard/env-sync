package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ptyhard/env-sync/internal/config"
)

// --version フラグの統合テスト（バイナリをビルドして実行）

func TestVersionFlag(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("--version 実行失敗: %s", err)
	}
	got := strings.TrimSpace(string(out))
	if !strings.HasPrefix(got, "env-sync version ") {
		t.Errorf("--version 出力: got %q, want prefix \"env-sync version \"", got)
	}
}

// ldflags でバージョン情報が実際に注入されることを検証する。
// シンボルは package main のため -X main.version で指定する必要がある。
// （フルインポートパス指定は一致せず黙殺されるため、その回帰を防ぐ）
func TestVersionFlag_LdflagsInjected(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	ldflags := "-X main.version=v9.9.9-test -X main.commit=deadbeef -X main.date=2026-01-01"
	if out, err := exec.Command("go", "build", "-ldflags", ldflags, "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("--version 実行失敗: %s", err)
	}
	got := strings.TrimSpace(string(out))
	want := "env-sync version v9.9.9-test (commit: deadbeef, built: 2026-01-01)"
	if got != want {
		t.Errorf("ldflags 注入が反映されていない: got %q, want %q", got, want)
	}
}

// ldflags 無しのビルドでも runtime/debug のフォールバックで
// 初期値の "dev" のままにならない（VCS 情報で補われる）ことを検証する。
// VCS 情報が埋め込まれない環境（.git が無い、-buildvcs=false 等）ではスキップする。
func TestVersionFlag_DebugFallback(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	// go version -m でバイナリに vcs.revision が埋め込まれているか確認する。
	// 埋め込まれていない環境（CI の checkout 状況や -buildvcs=false 等）ではスキップ。
	verOut, err := exec.Command("go", "version", "-m", bin).Output()
	if err != nil {
		t.Skipf("go version -m の実行に失敗したためスキップ: %s", err)
	}
	if !strings.Contains(string(verOut), "vcs.revision") {
		t.Skip("バイナリに vcs.revision が埋め込まれていないためスキップ（.git 無し / -buildvcs=false 等）")
	}

	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("--version 実行失敗: %s", err)
	}
	got := strings.TrimSpace(string(out))
	// commit がフォールバックで埋まり "none" のままでないこと。
	if strings.Contains(got, "commit: none") {
		t.Errorf("debug フォールバックが効いていない（commit が none のまま）: %q", got)
	}
}

func TestVersionFlag_ExitsZero(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}
	cmd := exec.Command(bin, "--version")
	if err := cmd.Run(); err != nil {
		t.Errorf("--version は exit 0 であるべき: %s", err)
	}
}

func TestHelpFlag_ExitsZero(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}
	cmd := exec.Command(bin, "--help")
	if err := cmd.Run(); err != nil {
		t.Errorf("--help は exit 0 であるべき: %s", err)
	}
}

func TestHelpFlag_IncludesEnvironmentsOption(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	cmd := exec.Command(bin, "--help")
	out, _ := cmd.CombinedOutput() // --help は exit 0 だが念のため err を無視
	if !strings.Contains(string(out), "--environments") {
		t.Errorf("--help 出力に --environments が含まれない:\n%s", string(out))
	}
}

// ---- collectAllowedEnvironments のユニットテスト ----

// TestCollectAllowedEnvironments_StandardsAlwaysAllowed は production/preview/development が
// def の内容に関わらず常に許容集合に含まれることを検証する。
func TestCollectAllowedEnvironments_StandardsAlwaysAllowed(t *testing.T) {
	def := config.Definition{} // 空の定義
	allowed := collectAllowedEnvironments(def)
	for _, name := range []string{"production", "preview", "development"} {
		if !allowed[name] {
			t.Errorf("標準環境 %q が許容集合に含まれない", name)
		}
	}
}

// TestCollectAllowedEnvironments_IncludesDefaultsEnvironments は
// defaults.environments に宣言した名前（Custom Environment 名）が許容集合に含まれることを検証する。
func TestCollectAllowedEnvironments_IncludesDefaultsEnvironments(t *testing.T) {
	def := config.Definition{}
	def.Defaults.Environments = []string{"staging", "canary"}
	allowed := collectAllowedEnvironments(def)
	for _, name := range []string{"staging", "canary"} {
		if !allowed[name] {
			t.Errorf("defaults.environments の %q が許容集合に含まれない", name)
		}
	}
}

// TestCollectAllowedEnvironments_IncludesVariableEnvironments は
// 各変数の environments に宣言した名前が許容集合に含まれることを検証する。
func TestCollectAllowedEnvironments_IncludesVariableEnvironments(t *testing.T) {
	def := config.Definition{
		Variables: map[string]config.VarConf{
			"DB_URL": {Environments: []string{"production", "staging"}},
			"DEBUG":  {Environments: []string{"preview"}},
		},
	}
	allowed := collectAllowedEnvironments(def)
	for _, name := range []string{"staging", "production", "preview"} {
		if !allowed[name] {
			t.Errorf("variables の environments の %q が許容集合に含まれない", name)
		}
	}
}

// TestCollectAllowedEnvironments_UnknownName_NotInAllowed は
// 定義ファイルにも標準3種にも無い名前が許容集合に含まれないことを検証する。
func TestCollectAllowedEnvironments_UnknownName_NotInAllowed(t *testing.T) {
	def := config.Definition{
		Variables: map[string]config.VarConf{
			"KEY": {Environments: []string{"staging"}},
		},
	}
	allowed := collectAllowedEnvironments(def)
	if allowed["totally-unknown"] {
		t.Error("定義に無い環境名 \"totally-unknown\" が許容集合に含まれてはならない")
	}
}

// TestEnvironmentsFlag_InvalidName_ErrorsOut は --environments に宣言外の名前を指定すると
// exit 1 かつ stderr にエラーメッセージが出ることをバイナリ統合テストで検証する。
func TestEnvironmentsFlag_InvalidName_ErrorsOut(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	dir := t.TempDir()
	envFile := dir + "/.env"
	defFile := dir + "/env-sync.yaml"
	if err := os.WriteFile(envFile, []byte("DB_URL=postgres://localhost/db\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// staging のみ宣言。totally-unknown は宣言外。
	defContent := "variables:\n  DB_URL:\n    secret: true\n    environments: [staging]\n"
	if err := os.WriteFile(defFile, []byte(defContent), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "--dry-run", "--env", envFile, "--def", defFile, "--environments", "totally-unknown")
	cmd.Env = append(os.Environ(), "VERCEL_PROJECT_ID=dummy-project")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("不正な --environments 値でも exit 0 になった（exit 1 を期待）:\n%s", out)
	}
	if !strings.Contains(string(out), "totally-unknown") {
		t.Errorf("エラー出力に不正な環境名 \"totally-unknown\" が含まれない:\n%s", out)
	}
}

// ---- --dry-run フラグのテスト ----

func TestDryRunFlag_NoTokenRequired(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	dir := t.TempDir()
	envFile := dir + "/.env"
	defFile := dir + "/env-sync.yaml"
	if err := os.WriteFile(envFile, []byte("FOO=bar\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// 新スキーマ: secret: false で plain 変数として登録
	if err := os.WriteFile(defFile, []byte("variables:\n  FOO: {secret: false}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "--dry-run", "--env", envFile, "--def", defFile)
	cmd.Env = append(os.Environ(), "VERCEL_PROJECT_ID=dummy-project")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("--dry-run は VERCEL_TOKEN なしで成功するべき: %s\n%s", err, out)
	}
	if !strings.Contains(string(out), "[dry-run]") {
		t.Errorf("dry-run 出力に [dry-run] が含まれない: %s", out)
	}
}
