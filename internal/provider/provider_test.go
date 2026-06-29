package provider_test

import (
	"testing"

	"github.com/ptyhard/env-sync/internal/provider"

	_ "github.com/ptyhard/env-sync/internal/provider/github"
	_ "github.com/ptyhard/env-sync/internal/provider/vercel"
)

// TestRegistry_VercelGitHubRegistered は vercel/github が registry に登録されているかを確認する。
func TestRegistry_VercelGitHubRegistered(t *testing.T) {
	for _, name := range []string{"vercel", "github"} {
		p, ok := provider.LookupProvider(name)
		if !ok {
			t.Errorf("LookupProvider(%q): ok = false, want true", name)
			continue
		}
		if p.Name() != name {
			t.Errorf("provider.Name() = %q, want %q", p.Name(), name)
		}
	}
}

// TestRegistry_UnknownProvider は未登録名で ok==false を返すことを確認する。
func TestRegistry_UnknownProvider(t *testing.T) {
	_, ok := provider.LookupProvider("nonexistent-provider-xyz")
	if ok {
		t.Error("LookupProvider(未登録名): ok = true, want false")
	}
}

// TestRegisteredProviderNames_Contains は RegisteredProviderNames が vercel / github を含むことを確認する。
// init() 実行順は Go 仕様で保証されないため、順序ではなく存在のみを検証する。
func TestRegisteredProviderNames_Contains(t *testing.T) {
	names := provider.RegisteredProviderNames()
	foundVercel, foundGitHub := false, false
	for _, n := range names {
		switch n {
		case "vercel":
			foundVercel = true
		case "github":
			foundGitHub = true
		}
	}
	if !foundVercel {
		t.Error("vercel が RegisteredProviderNames に含まれない")
	}
	if !foundGitHub {
		t.Error("github が RegisteredProviderNames に含まれない")
	}
}

// TestProvider_MockReplaceable はテスト用 mockProvider を registry に一時登録し、
// interface 越しに差し替え可能であることを検証する。
func TestProvider_MockReplaceable(t *testing.T) {
	const mockName = "mock-provider-test-pkg"

	called := false
	mock := &mockProvider{
		name: mockName,
		syncFn: func(opts provider.Options, entries []provider.Entry) error {
			called = true
			return nil
		},
	}

	// 一時登録
	provider.RegisterProvider(mockName, func() provider.Provider { return mock })
	t.Cleanup(func() {
		reg, order := provider.RegistryForTest()
		delete(reg, mockName)
		for i, n := range *order {
			if n == mockName {
				*order = append((*order)[:i], (*order)[i+1:]...)
				break
			}
		}
	})

	p, ok := provider.LookupProvider(mockName)
	if !ok {
		t.Fatal("mockProvider の lookup に失敗")
	}
	if p.Name() != mockName {
		t.Errorf("Name() = %q, want %q", p.Name(), mockName)
	}

	// Sync が呼ばれることを確認
	err := p.Sync(provider.Options{}, nil)
	if err != nil {
		t.Errorf("Sync() エラー: %v", err)
	}
	if !called {
		t.Error("Sync() が呼ばれなかった")
	}
}

// mockProvider はテスト専用の Provider 実装。
type mockProvider struct {
	name   string
	syncFn func(opts provider.Options, entries []provider.Entry) error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Sync(opts provider.Options, entries []provider.Entry) error {
	return m.syncFn(opts, entries)
}

// TestValidator_OptionalInterface は Validator インターフェースの型アサーションが機能することを確認する。
// Validator は任意インターフェースなので、実装しない Provider は型アサーションが false になる。
func TestValidator_OptionalInterface(t *testing.T) {
	// Validate を実装しない mockProvider は Validator ではない
	mock := &mockProvider{name: "mock", syncFn: func(_ provider.Options, _ []provider.Entry) error { return nil }}
	_, ok := any(mock).(provider.Validator)
	if ok {
		t.Error("Validator 未実装の mockProvider が Validator として型アサーションされてはいけない")
	}

	// Validate を実装した mockValidatorProvider は Validator になる
	v := &mockValidatorProvider{}
	_, ok = any(v).(provider.Validator)
	if !ok {
		t.Error("Validator を実装した mockValidatorProvider が provider.Validator として型アサーションできない")
	}
}

// mockValidatorProvider は Validate を実装したテスト専用 Provider。
type mockValidatorProvider struct{}

func (m *mockValidatorProvider) Name() string                                      { return "mock-validator" }
func (m *mockValidatorProvider) Sync(_ provider.Options, _ []provider.Entry) error { return nil }
func (m *mockValidatorProvider) Validate(_ provider.Options, _ []provider.Entry) error {
	return nil
}
