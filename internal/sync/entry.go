// Package sync は定義ファイルと env 値から provider.Entry スライスへの変換ロジックを提供する。
package sync

import (
	"fmt"
	"strings"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// ResolveEntries は def と envVars から provider.Entry のスライスを生成する。
// cliProvider は --provider フラグの値で、YAML に provider が書かれていない場合のデフォルトとなる。
//   - def にあるが envVars に無いキーはスキップする
//   - secret: VarConf.Secret が非 nil → その値、nil → defaults.Secret が非 nil → その値、nil → true
//   - environments: VarConf.Environments が非空 → その値、空 → defaults.Environments が非空 → その値、空 → 空のまま
//     空文字列エントリは除去し、重複は除去してから Entry に反映する。
//   - providers: VarConf.Provider → defaults.Provider → cliProvider の優先順位で解決する。
//     不正なプロバイダー値はエラーを返す。
func ResolveEntries(def config.Definition, envVars map[string]string, defKeys []string, cliProvider string) ([]provider.Entry, error) {
	// defaults.provider の値を事前検証する。varConf で上書きされても不正値は許容しない。
	if def.Defaults.Provider != nil {
		for _, p := range def.Defaults.Provider.Values {
			if trimmed := strings.TrimSpace(p); trimmed != "" && !provider.IsRegisteredProvider(trimmed) {
				names := strings.Join(provider.RegisteredProviderNames(), " / ")
				return nil, fmt.Errorf("%s", i18n.T(i18n.MsgDefaultsProviderInvalid, trimmed, names))
			}
		}
	}

	var entries []provider.Entry
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
				names := strings.Join(provider.RegisteredProviderNames(), " / ")
				return nil, fmt.Errorf("%s", i18n.T(i18n.MsgDefaultsProviderEmpty, names))
			}
			providers = def.Defaults.Provider.Values
		}
		if conf.Provider != nil {
			if len(conf.Provider.Values) == 0 {
				names := strings.Join(provider.RegisteredProviderNames(), " / ")
				return nil, fmt.Errorf("%s", i18n.T(i18n.MsgVarProviderEmpty, key, names))
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
			names := strings.Join(provider.RegisteredProviderNames(), " / ")
			return nil, fmt.Errorf("%s", i18n.T(i18n.MsgVarProviderBlank, key, names))
		}

		// provider 値の検証
		for _, p := range providers {
			if !provider.IsRegisteredProvider(p) {
				names := strings.Join(provider.RegisteredProviderNames(), " / ")
				return nil, fmt.Errorf("%s", i18n.T(i18n.MsgVarProviderInvalid, key, p, names))
			}
		}

		// vercel_project の解決: varConf.VercelProject → defaults.VercelProject
		var vercelProjects []string
		if def.Defaults.VercelProject != nil {
			vercelProjects = def.Defaults.VercelProject.Values
		}
		if conf.VercelProject != nil {
			vercelProjects = conf.VercelProject.Values
		}

		entries = append(entries, provider.Entry{
			Key:            key,
			Value:          val,
			Secret:         secret,
			Environments:   envs,
			Providers:      providers,
			VercelProjects: vercelProjects,
		})
	}
	return entries, nil
}

// FilterEntriesByEnvironments は filterEnvs（--environments フラグの値）で entries を絞り込む。
// filterEnvs が空のとき entries をそのまま返す（後方互換）。
// filterEnvs が非空のとき:
//   - entry.Environments が nil/空（宣言なし）→ 「全環境」ではなく「宣言なし」とみなしスキップ
//     （Vercel は envs 空だと暗黙で [production, preview] に書くため、nil のまま通すと
//     フラグ外環境へ書く事故になる。安全側に倒してスキップする）
//   - entry.Environments ∩ filterEnvs が空 → スキップ
//   - 積集合が非空 → entry の Environments を積集合に置き換えて返す
//
// 返り値: (filtered []provider.Entry, skippedKeys []string)
func FilterEntriesByEnvironments(entries []provider.Entry, filterEnvs []string) ([]provider.Entry, []string) {
	if len(filterEnvs) == 0 {
		return entries, nil
	}
	filterSet := make(map[string]bool, len(filterEnvs))
	for _, e := range filterEnvs {
		filterSet[e] = true
	}
	filtered := make([]provider.Entry, 0, len(entries))
	var skipped []string
	for _, e := range entries {
		// environments が nil/空 → 宣言なし → filterEnvs 指定時はスキップ
		if len(e.Environments) == 0 {
			skipped = append(skipped, e.Key)
			continue
		}
		var intersection []string
		for _, env := range e.Environments {
			if filterSet[env] {
				intersection = append(intersection, env)
			}
		}
		if len(intersection) == 0 {
			skipped = append(skipped, e.Key)
			continue
		}
		// 積集合で Environments を上書き
		e.Environments = intersection
		filtered = append(filtered, e)
	}
	return filtered, skipped
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
