package main

// Provider は同期先を抽象化するインターフェース。
// 各プロバイダは init() で registerProvider を呼び自己登録する。
type Provider interface {
	// Name はプロバイダ名（"vercel" / "github" 等）を返す。
	Name() string
	// Sync は entries を同期する。
	// os.Exit(1) は Sync 内にそのまま残し、挙動同一を最優先とする。
	Sync(opts options, entries []Entry) error
}

// providerRegistry は名前 → ファクトリ関数のマップ。
var providerRegistry = map[string]func() Provider{}

// providerOrder は登録順を保持するスライス（registeredProviderNames の順序保証）。
var providerOrder []string

// registerProvider はプロバイダ名とファクトリ関数を registry に登録する。
// 各プロバイダの init() から呼び出す。
// 同名プロバイダを二重登録した場合は panic する。
func registerProvider(name string, factory func() Provider) {
	if _, exists := providerRegistry[name]; exists {
		panic("registerProvider: プロバイダ " + name + " は既に登録されています")
	}
	providerRegistry[name] = factory
	providerOrder = append(providerOrder, name)
}

// lookupProvider は name に対応する Provider を返す。
// 登録されていない場合は (nil, false) を返す。
func lookupProvider(name string) (Provider, bool) {
	factory, ok := providerRegistry[name]
	if !ok {
		return nil, false
	}
	return factory(), true
}

// registeredProviderNames は登録済みプロバイダ名を登録順で返す。
// parseFlags の検証エラー文言生成で使用する。
func registeredProviderNames() []string {
	result := make([]string, len(providerOrder))
	copy(result, providerOrder)
	return result
}
