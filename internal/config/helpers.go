package config

import (
	"os"
	"sort"

	"golang.org/x/term"
)

// FileExists はパスが存在するかを返す。
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsTTY はファイルディスクリプタが端末かを返す。
func IsTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// SortedKeys は map[string]VarConf のキーをソートして返す。
func SortedKeys(m map[string]VarConf) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SortedStrKeys は map[string]string のキーをソートして返す。
func SortedStrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
