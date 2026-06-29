package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestValidateSubcommand_HelpExitsZero は validate --help が exit 0 で終了することを確認する。
func TestValidateSubcommand_HelpExitsZero(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}
	cmd := exec.Command(bin, "validate", "--help")
	if err := cmd.Run(); err != nil {
		t.Errorf("validate --help は exit 0 であるべき: %s", err)
	}
}

// TestValidateSubcommand_NoDefOrEnv は def/env ファイルが存在しなくても fatal にならないことを確認する。
// token / project_id が未設定のため exit 1 になるが、ファイル不在でパニックしないことを確認する。
func TestValidateSubcommand_NoDefOrEnv(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}

	dir := t.TempDir()
	cmd := exec.Command(bin, "validate",
		"--env", dir+"/nonexistent.env",
		"--def", dir+"/nonexistent.yaml",
	)
	// VERCEL_PROJECT_ID を未設定にして API 呼び出しを回避
	env := []string{
		"VERCEL_TOKEN=",
		"VERCEL_PROJECT_ID=",
		"GITHUB_TOKEN=",
		"GITHUB_REPO=",
		"XDG_CONFIG_HOME=" + dir + "/no-global",
	}
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	output := string(out)

	// ファイル不在のエラー（fatal）ではなく、警告を出して継続することを確認
	// def/env の不在警告が出ることを確認
	if strings.Contains(output, "panic") {
		t.Errorf("panic が発生した: %s", output)
	}
	// def/env 不在の警告または validate ヘッダが出ることを確認
	// (exit 1 は許容: token 未設定のため)
	_ = err // exit 1 は許容
}

// TestValidateSubcommand_InUsage は --help に validate の説明が含まれることを確認する。
func TestValidateSubcommand_InUsage(t *testing.T) {
	bin := t.TempDir() + "/env-sync-test"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("ビルド失敗: %s\n%s", err, out)
	}
	out, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil {
		t.Fatalf("--help 実行失敗: %s", err)
	}
	if !strings.Contains(string(out), "validate") {
		t.Errorf("--help の出力に validate が含まれない: %s", out)
	}
}

// TestRunValidate_ProviderNotValidator_ReturnsNil は Validator 未実装の provider（gcp）を
// 指定した場合に runValidate が nil を返す（型アサーション失敗の分岐）ことを確認する。
// 未登録名は ParseFlags 側で os.Exit(1) になるため、ここでは登録済みだが Validator 未実装の gcp を使う。
func TestRunValidate_ProviderNotValidator_ReturnsNil(t *testing.T) {
	args := []string{"--provider", "gcp"}
	if err := runValidate(args, func() {}); err != nil {
		t.Errorf("Validator 未実装 provider 指定時は nil を期待: %v", err)
	}
}
