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
	// サブディレクトリの実行権限を落とすことで os.Stat が EACCES になる状況を作る。
	// （ファイル自体の chmod では os.Stat は失敗しない）
	parent := t.TempDir()
	sub, err := os.MkdirTemp(parent, "noperm-*")
	if err != nil {
		t.Skip("サブディレクトリ作成失敗")
	}
	f, err := os.CreateTemp(sub, "file-*.txt")
	if err != nil {
		t.Skip("一時ファイル作成失敗")
	}
	f.Close()
	path := f.Name()
	// ディレクトリの実行権限を落として中のファイルに stat できなくする
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Skip("chmod 失敗")
	}
	t.Cleanup(func() { os.Chmod(sub, 0o700) }) // TempDir の掃除が通るよう権限を戻す
	// ファイルは存在するが stat できない → FileExists は true を返すべき
	if !FileExists(path) {
		t.Error("FileExists(EACCES) = false, want true（ファイルは存在する）")
	}
}
