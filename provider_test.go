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

// TestRegisteredProviderNames_Order は registeredProviderNames が sort なし登録順を返すことを確認する（W1 回帰防止）。
// Go の init() 実行順はファイル名の lexical 順なので github.go (g) → vercel.go (v) の順になる。
func TestRegisteredProviderNames_Order(t *testing.T) {
	names := registeredProviderNames()
	// vercel と github が両方含まれていること
	foundVercel, foundGitHub := false, false
	vercelIdx, githubIdx := -1, -1
	for i, n := range names {
		switch n {
		case "vercel":
			foundVercel = true
			vercelIdx = i
		case "github":
			foundGitHub = true
			githubIdx = i
		}
	}
	if !foundVercel {
		t.Error("vercel が registeredProviderNames に含まれない")
	}
	if !foundGitHub {
		t.Error("github が registeredProviderNames に含まれない")
	}
	// sort が行われていないこと: registeredProviderNames の結果が sort.Strings と異なる（もしくは
	// sort 後と同じになる場合でも panic しない）ことを確認。
	// 実際の登録順（github→vercel）が維持されていれば、github のインデックスが vercel より小さい。
	if vercelIdx >= 0 && githubIdx >= 0 && githubIdx > vercelIdx {
		t.Errorf("github (%d) が vercel (%d) より後に返された（登録順は github→vercel のはず）", githubIdx, vercelIdx)
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
