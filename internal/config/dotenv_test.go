package config

import "testing"

// ParseDotenv のテスト

func TestParseDotenv_Basic(t *testing.T) {
	input := "FOO=bar\nBAZ=qux\n"
	got := ParseDotenv(input)
	if got["FOO"] != "bar" {
		t.Errorf("FOO: got %q, want bar", got["FOO"])
	}
	if got["BAZ"] != "qux" {
		t.Errorf("BAZ: got %q, want qux", got["BAZ"])
	}
}

func TestParseDotenv_SkipsComments(t *testing.T) {
	input := "# comment\nFOO=bar\n"
	got := ParseDotenv(input)
	if _, ok := got["# comment"]; ok {
		t.Error("コメント行がキーとして解釈された")
	}
	if got["FOO"] != "bar" {
		t.Errorf("FOO: got %q, want bar", got["FOO"])
	}
}

func TestParseDotenv_QuotedValues(t *testing.T) {
	input := `FOO="hello world"` + "\nBAR='single'\n"
	got := ParseDotenv(input)
	if got["FOO"] != "hello world" {
		t.Errorf("ダブルクォート: got %q, want \"hello world\"", got["FOO"])
	}
	if got["BAR"] != "single" {
		t.Errorf("シングルクォート: got %q, want single", got["BAR"])
	}
}

func TestParseDotenv_ExportPrefix(t *testing.T) {
	input := "export FOO=bar\n"
	got := ParseDotenv(input)
	if got["FOO"] != "bar" {
		t.Errorf("export 付き: got %q, want bar", got["FOO"])
	}
}

func TestParseDotenv_EmptyLines(t *testing.T) {
	input := "\n\nFOO=bar\n\n"
	got := ParseDotenv(input)
	if len(got) != 1 {
		t.Errorf("空行を含むとき: got %d keys, want 1", len(got))
	}
}
