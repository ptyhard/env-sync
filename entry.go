package main

import "strings"

// Entry は provider 非依存の共通ドメインモデル。
// 登録する環境変数 1 件分の情報を保持する。
type Entry struct {
	Key          string
	Value        string
	Secret       bool
	Environments []string
}

// resolveEntries は def と envVars から Entry のスライスを生成する。
//   - def にあるが envVars に無いキーはスキップする
//   - secret: varConf.Secret が非 nil → その値、nil → defaults.Secret が非 nil → その値、nil → true
//   - environments: varConf.Environments が非空 → その値、空 → defaults.Environments が非空 → その値、空 → 空のまま
//     空文字列エントリは除去し、重複は除去してから Entry に反映する。
func resolveEntries(def definition, envVars map[string]string, defKeys []string) ([]Entry, error) {
	var entries []Entry
	for _, key := range defKeys {
		val, ok := envVars[key]
		if !ok {
			continue
		}
		conf := def.Variables[key]

		// secret の解決
		secret := true // 安全側デフォルト
		if def.Defaults.Secret != nil {
			secret = *def.Defaults.Secret
		}
		if conf.Secret != nil {
			secret = *conf.Secret
		}

		// environments の解決
		// nil = 未指定（YAML に書いていない）、非 nil = 明示指定（空配列 [] も含む）として区別する。
		// varConf が nil でないときは defaults より優先して採用し、明示空でも defaults を上書きできる。
		var envs []string
		if def.Defaults.Environments != nil {
			envs = def.Defaults.Environments
		}
		if conf.Environments != nil {
			envs = conf.Environments
		}

		// 空文字列を除去し重複を排除する
		envs = deduplicateEnvironments(envs)

		entries = append(entries, Entry{
			Key:          key,
			Value:        val,
			Secret:       secret,
			Environments: envs,
		})
	}
	return entries, nil
}

// deduplicateEnvironments は environments スライスから空文字・空白のみの要素を除去し重複を排除する。
// 入力が空なら nil を返す（provider 側フォールバックが空スライスかどうかを len で判定するため）。
func deduplicateEnvironments(envs []string) []string {
	if len(envs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(envs))
	result := make([]string, 0, len(envs))
	for _, e := range envs {
		trimmed := strings.TrimSpace(e)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
