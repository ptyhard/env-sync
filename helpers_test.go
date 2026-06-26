package main

import (
	"os"
	"testing"
)

func TestFileExists_Exists(t *testing.T) {
	// go.mod は必ず存在するファイルとして使用する
	if !fileExists("go.mod") {
		t.Error("fileExists(go.mod) = false, want true")
	}
}

func TestFileExists_NotExists(t *testing.T) {
	if fileExists("__nonexistent_file_xyz__.txt") {
		t.Error("fileExists(存在しないファイル) = true, want false")
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]varConf{
		"zz": {},
		"aa": {},
		"mm": {},
	}
	keys := sortedKeys(m)
	want := []string{"aa", "mm", "zz"}
	if len(keys) != len(want) {
		t.Fatalf("sortedKeys len = %d, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("sortedKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestSortedStrKeys(t *testing.T) {
	m := map[string]string{
		"zz": "v1",
		"aa": "v2",
		"mm": "v3",
	}
	keys := sortedStrKeys(m)
	want := []string{"aa", "mm", "zz"}
	if len(keys) != len(want) {
		t.Fatalf("sortedStrKeys len = %d, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Errorf("sortedStrKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestFileExists_PermError(t *testing.T) {
	// 権限 000 のファイルは存在するが Stat では成功する（読み取り不可だが stat は可能）
	// ここでは単に既存ファイルが存在検出できることを確認するにとどめる
	f, err := os.CreateTemp(t.TempDir(), "test-*.txt")
	if err != nil {
		t.Skip("一時ファイル作成失敗")
	}
	f.Close()
	if !fileExists(f.Name()) {
		t.Errorf("fileExists(一時ファイル) = false, want true")
	}
}
