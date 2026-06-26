package config

import (
	"os"
	"testing"
)

func TestFileExists_Exists(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "exists-*.txt")
	if err != nil {
		t.Skip("一時ファイル作成失敗")
	}
	f.Close()
	if !FileExists(f.Name()) {
		t.Error("FileExists(一時ファイル) = false, want true")
	}
}

func TestFileExists_NotExists(t *testing.T) {
	if FileExists("__nonexistent_file_xyz__.txt") {
		t.Error("FileExists(存在しないファイル) = true, want false")
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]VarConf{
		"zz": {},
		"aa": {},
		"mm": {},
	}
	keys := SortedKeys(m)
	want := []string{"aa", "mm", "zz"}
	if len(keys) != len(want) {
		t.Fatalf("SortedKeys len = %d, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("SortedKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestSortedStrKeys(t *testing.T) {
	m := map[string]string{
		"zz": "v1",
		"aa": "v2",
		"mm": "v3",
	}
	keys := SortedStrKeys(m)
	want := []string{"aa", "mm", "zz"}
	if len(keys) != len(want) {
		t.Fatalf("SortedStrKeys len = %d, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("SortedStrKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestFileExists_PermError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root では権限エラーを再現できない")
	}
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "perm-*.txt")
	if err != nil {
		t.Skip("一時ファイル作成失敗")
	}
	f.Close()
	// ファイルのパーミッションを 000 にして読み取り不能にする
	if err := os.Chmod(f.Name(), 0o000); err != nil {
		t.Skip("chmod 失敗")
	}
	// ファイルは存在するが stat でアクセスできない → FileExists は true を返すべき
	if !FileExists(f.Name()) {
		t.Error("FileExists(権限なしファイル) = false, want true（ファイルは存在する）")
	}
}
