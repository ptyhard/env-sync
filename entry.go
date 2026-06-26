package main

import (
	"fmt"
	"strings"
)

// Entry は provider 非依存の共通ドメインモデル。
// 登録する環境変数 1 件分の情報を保持する。
type Entry struct {
	Key          string
	Value        string
	Secret       bool
	Environments []string
	Providers    []string // 同期先プロバイダーのリスト（"vercel" / "github" など）
}

// resolveEntries は def と envVars から Entry のスライスを生成する。
// cliProvider は --provider フラグの値で、YAML に provider が書かれていない場合のデフォルトとなる。
//   - def にあるが envVars に無いキーはスキップする
//   - secret: varConf.Secret が非 nil → その値、nil → defaults.Secret が非 nil → その値、nil → true
//   - environments: varConf.Environments が非空 → その値、空 → defaults.Environments が非空 → その値、空 → 空のまま
//     空文字列エントリは除去し、重複は除去してから Entry に反映する。
//   - providers: varConf.Provider → defaults.Provider → cliProvider の優先順位で解決する。
//     不正なプロバイダー値はエラーを返す。
func resolveEntries(def definition, envVars map[string]string, defKeys []string, cliProvider string) ([]Entry, error) {
	// defaults.provider の値を事前検証する。varConf で上書きされても不正値は許容しない。
	if def.Defaults.Provider != nil {
		for _, p := range def.Defaults.Provider.Values {
			if trimmed := strings.TrimSpace(p); trimmed != "" && !isRegisteredProvider(trimmed) {
				names := strings.Join(registeredProviderNames(), " / ")
				return nil, fmt.Errorf("defaults.provider: 不正な provider 値 %q（%s のいずれかを指定してください）", trimmed, names)
			}
		}
	}

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

		// provider の解決: varConf.Provider → defaults.Provider → CLI フラグ
		var providers []string
		if def.Defaults.Provider != nil {
			if len(def.Defaults.Provider.Values) == 0 {
				names := strings.Join(registeredProviderNames(), " / ")
				return nil, fmt.Errorf("defaults.provider に空配列が指定されています（%s のいずれかを指定してください）", names)
			}
			providers = def.Defaults.Provider.Values
		}
		if conf.Provider != nil {
			if len(conf.Provider.Values) == 0 {
				names := strings.Join(registeredProviderNames(), " / ")
				return nil, fmt.Errorf("%s: provider に空配列が指定されています（%s のいずれかを指定してください）", key, names)
			}
			providers = conf.Provider.Values
		}
		if len(providers) == 0 {
			providers = []string{cliProvider}
		}
		// [vercel, vercel] のような重複指定で二重 Sync にならないよう排除する
		providers = deduplicateProviders(providers)
		// dedup 後に空になった場合（例: provider: " "）は設定ミスとしてエラー
		if len(providers) == 0 {
			names := strings.Join(registeredProviderNames(), " / ")
			return nil, fmt.Errorf("%s: provider の指定が空または空白のみです（%s のいずれかを指定してください）", key, names)
		}

		// provider 値の検証
		for _, p := range providers {
			if !isRegisteredProvider(p) {
				names := strings.Join(registeredProviderNames(), " / ")
				return nil, fmt.Errorf("%s: 不正な provider 値 %q（%s のいずれかを指定してください）", key, p, names)
			}
		}

		entries = append(entries, Entry{
			Key:          key,
			Value:        val,
			Secret:       secret,
			Environments: envs,
			Providers:    providers,
		})
	}
	return entries, nil
}

// deduplicateProviders は providers スライスから空文字・空白のみの要素を除去し重複を排除する。
// [vercel, vercel] のような重複指定を正規化し、二重 Sync を防ぐ。
func deduplicateProviders(providers []string) []string {
	if len(providers) == 0 {
		return providers
	}
	seen := make(map[string]bool, len(providers))
	result := make([]string, 0, len(providers))
	for _, p := range providers {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		result = append(result, trimmed)
	}
	return result
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
