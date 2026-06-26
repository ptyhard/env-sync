package provider

// RegistryForTest はテスト専用の registry アクセサ。
// _test.go サフィックスによりテストビルド時のみコンパイルされ、本番バイナリの API 面を汚さない。
func RegistryForTest() (map[string]func() Provider, *[]string) {
	return providerRegistry, &providerOrder
}
