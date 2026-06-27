package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
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
