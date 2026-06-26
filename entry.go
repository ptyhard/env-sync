package main

// Entry は provider 非依存の共通ドメインモデル。
// 登録する環境変数 1 件分の情報を保持する。
type Entry struct {
	Key          string
	Value        string
	Secret       bool
	Environments []string
}

// resolveEntries は def と envVars から Entry のスライスを生成する。
// - def にあるが envVars に無いキーはスキップする
// - secret: varConf.Secret が非 nil → その値、nil → defaults.Secret が非 nil → その値、nil → true
// - environments: varConf.Environments が非空 → その値、空 → defaults.Environments が非空 → その値、空 → 空のまま
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
		var envs []string
		if len(def.Defaults.Environments) > 0 {
			envs = def.Defaults.Environments
		}
		if len(conf.Environments) > 0 {
			envs = conf.Environments
		}

		entries = append(entries, Entry{
			Key:          key,
			Value:        val,
			Secret:       secret,
			Environments: envs,
		})
	}
	return entries, nil
}
