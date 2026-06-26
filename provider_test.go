package main

import "testing"

// TestRegistry_VercelGitHubRegistered は vercel/github が registry に登録されているかを確認する。
func TestRegistry_VercelGitHubRegistered(t *testing.T) {
	for _, name := range []string{"vercel", "github"} {
		p, ok := lookupProvider(name)
		if !ok {
			t.Errorf("lookupProvider(%q): ok = false, want true", name)
			continue
		}
		if p.Name() != name {
			t.Errorf("provider.Name() = %q, want %q", p.Name(), name)
		}
	}
}

// TestRegistry_UnknownProvider は未登録名で ok==false を返すことを確認する。
func TestRegistry_UnknownProvider(t *testing.T) {
	_, ok := lookupProvider("nonexistent-provider-xyz")
	if ok {
		t.Error("lookupProvider(未登録名): ok = true, want false")
	}
}

// TestRegisteredProviderNames_Contains は registeredProviderNames が vercel / github を含むことを確認する。
// init() 実行順は Go 仕様で保証されないため、順序ではなく存在のみを検証する。
func TestRegisteredProviderNames_Contains(t *testing.T) {
	names := registeredProviderNames()
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
		t.Error("vercel が registeredProviderNames に含まれない")
	}
	if !foundGitHub {
		t.Error("github が registeredProviderNames に含まれない")
	}
}

// TestProvider_MockReplaceable はテスト用 mockProvider を registry に一時登録し、
// interface 越しに差し替え可能であることを検証する。
func TestProvider_MockReplaceable(t *testing.T) {
	const mockName = "mock-provider-test"

	called := false
	mock := &mockProvider{
		name: mockName,
		syncFn: func(opts options, entries []Entry) error {
			called = true
			return nil
		},
	}

	// 一時登録
	registerProvider(mockName, func() Provider { return mock })
	t.Cleanup(func() {
		delete(providerRegistry, mockName)
		// providerOrder からも削除
		for i, n := range providerOrder {
			if n == mockName {
				providerOrder = append(providerOrder[:i], providerOrder[i+1:]...)
				break
			}
		}
	})

	p, ok := lookupProvider(mockName)
	if !ok {
		t.Fatal("mockProvider の lookup に失敗")
	}
	if p.Name() != mockName {
		t.Errorf("Name() = %q, want %q", p.Name(), mockName)
	}

	// Sync が呼ばれることを確認
	err := p.Sync(options{}, nil)
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
	syncFn func(opts options, entries []Entry) error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Sync(opts options, entries []Entry) error {
	return m.syncFn(opts, entries)
}
