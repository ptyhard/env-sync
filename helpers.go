package main

import (
	"os"
	"sort"

	"golang.org/x/term"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func sortedKeys(m map[string]varConf) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStrKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
