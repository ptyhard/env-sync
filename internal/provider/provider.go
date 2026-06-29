package provider

// Options はコマンドラインフラグの値を保持する。
// cmd/env-sync から provider へ渡すための共通型。
type Options struct {
	Env           string
	Def           string
	DryRun        bool
	Yes           bool
	Provider      string
	VercelProject string // --vercel-project で指定した場合のターゲット名（モノレポ対応）
	GitHubRepo    string // --github-repo で指定した場合のターゲット名（モノレポ対応）
	Language      string // --lang で指定した表示言語コード（"en" / "ja"）
}

// Entry は provider 非依存の共通ドメインモデル。
// 登録する環境変数 1 件分の情報を保持する。
type Entry struct {
	Key            string
	Value          string
	Secret         bool
	Environments   []string
	Providers      []string // 同期先プロバイダーのリスト（"vercel" / "github" など）
	VercelProjects []string // 送信先 Vercel プロジェクト名のリスト（未指定なら全ターゲット）
}

// Provider は同期先を抽象化するインターフェース。
// 各プロバイダは init() で RegisterProvider を呼び自己登録する。
type Provider interface {
	// Name はプロバイダ名（"vercel" / "github" 等）を返す。
	Name() string
	// Sync は entries を同期する。
	Sync(opts Options, entries []Entry) error
}

// Validator は読み取り専用で認証・ターゲット解決を検証できる provider が実装する任意インターフェース。
// Validate は GET のみを使用し、環境変数の登録・変更を行わない。
type Validator interface {
	Validate(opts Options, entries []Entry) error
}

// providerRegistry は名前 → ファクトリ関数のマップ。
var providerRegistry = map[string]func() Provider{}

// providerOrder は登録順を保持するスライス（RegisteredProviderNames の順序保証）。
var providerOrder []string

// RegisterProvider はプロバイダ名とファクトリ関数を registry に登録する。
// 各プロバイダの init() から呼び出す。
// 同名プロバイダを二重登録した場合は panic する。
func RegisterProvider(name string, factory func() Provider) {
	if _, exists := providerRegistry[name]; exists {
		panic("RegisterProvider: プロバイダ " + name + " は既に登録されています")
	}
	providerRegistry[name] = factory
	providerOrder = append(providerOrder, name)
}

// LookupProvider は name に対応する Provider を返す。
// 登録されていない場合は (nil, false) を返す。
func LookupProvider(name string) (Provider, bool) {
	factory, ok := providerRegistry[name]
	if !ok {
		return nil, false
	}
	return factory(), true
}

// RegisteredProviderNames は登録済みプロバイダ名を登録順で返す。
func RegisteredProviderNames() []string {
	result := make([]string, len(providerOrder))
	copy(result, providerOrder)
	return result
}

// IsRegisteredProvider は name が registry に登録されているかを返す。
func IsRegisteredProvider(name string) bool {
	_, ok := providerRegistry[name]
	return ok
}
