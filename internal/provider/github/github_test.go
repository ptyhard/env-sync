package github

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/nacl/box"
	"gopkg.in/yaml.v3"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/provider"
)

// --- resolveGitHubRepo の GITHUB_REPO パーステスト ---

func TestResolveGitHubRepo_FromEnv(t *testing.T) {
	tests := []struct {
		name      string
		repoEnv   string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"正常", "owner/repo", "owner", "repo", false},
		{"前後空白あり", "  owner / repo  ", "owner", "repo", false},
		{"3 セグメントは不正", "owner/repo/extra", "", "", true},
		{"スラッシュなしは不正", "ownerrepo", "", "", true},
		{"owner 空は不正", "/repo", "", "", true},
		{"repo 空は不正", "owner/", "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GITHUB_REPO", tc.repoEnv)
			owner, repo, err := resolveGitHubRepo()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("エラーを期待したが nil（owner=%q repo=%q）", owner, repo)
				}
				return
			}
			if err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if owner != tc.wantOwner || repo != tc.wantRepo {
				t.Errorf("owner/repo = %q/%q, want %q/%q", owner, repo, tc.wantOwner, tc.wantRepo)
			}
		})
	}
}

// --- repoFromGitRemote のパーサテスト ---

func TestParseGitHubRemoteURL(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{
			name:      "SSH 形式 .git あり",
			rawURL:    "git@github.com:owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "SSH 形式 .git なし",
			rawURL:    "git@github.com:owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "HTTPS 形式 .git あり",
			rawURL:    "https://github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "HTTPS 形式 .git なし",
			rawURL:    "https://github.com/owner/repo",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:   "GitHub 以外 SSH",
			rawURL: "git@gitlab.com:owner/repo.git",
			wantOK: false,
		},
		{
			name:   "GitHub 以外 HTTPS",
			rawURL: "https://gitlab.com/owner/repo.git",
			wantOK: false,
		},
		{
			name:   "空文字",
			rawURL: "",
			wantOK: false,
		},
		{
			name:      "ユーザー名にハイフン",
			rawURL:    "git@github.com:my-org/my-repo.git",
			wantOwner: "my-org",
			wantRepo:  "my-repo",
			wantOK:    true,
		},
		{
			name:      "ssh:// 形式 .git あり",
			rawURL:    "ssh://git@github.com/owner/repo.git",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "HTTPS 形式 末尾スラッシュ",
			rawURL:    "https://github.com/owner/repo/",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
		{
			name:      "末尾改行を含む",
			rawURL:    "git@github.com:owner/repo.git\n",
			wantOwner: "owner",
			wantRepo:  "repo",
			wantOK:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, ok := parseGitHubRemoteURL(tc.rawURL)
			if ok != tc.wantOK {
				t.Errorf("ok = %v, want %v (url: %q)", ok, tc.wantOK, tc.rawURL)
				return
			}
			if !ok {
				return
			}
			if owner != tc.wantOwner {
				t.Errorf("owner = %q, want %q", owner, tc.wantOwner)
			}
			if repo != tc.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tc.wantRepo)
			}
		})
	}
}

// --- encryptSecret の round-trip テスト ---

func TestEncryptSecret_RoundTrip(t *testing.T) {
	// テスト用に鍵ペアを生成
	pubKey, privKey, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("鍵ペア生成失敗: %v", err)
	}

	original := "my-secret-value-123!@#"

	// 暗号化
	encrypted, err := encryptSecret(original, pubKey)
	if err != nil {
		t.Fatalf("encryptSecret 失敗: %v", err)
	}

	if encrypted == "" {
		t.Fatal("暗号化結果が空")
	}
	if encrypted == original {
		t.Fatal("暗号化されていない（元の値と同じ）")
	}

	// base64 デコードして復号
	decoded, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("base64 デコード失敗: %v", err)
	}

	decrypted, ok := box.OpenAnonymous(nil, decoded, pubKey, privKey)
	if !ok {
		t.Fatal("復号失敗（box.OpenAnonymous が false を返した）")
	}

	if string(decrypted) != original {
		t.Errorf("復号結果 = %q, want %q", string(decrypted), original)
	}
}

func TestEncryptSecret_EmptyValue(t *testing.T) {
	pubKey, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("鍵ペア生成失敗: %v", err)
	}

	encrypted, err := encryptSecret("", pubKey)
	if err != nil {
		t.Fatalf("空文字の暗号化でエラー: %v", err)
	}
	if encrypted == "" {
		t.Fatal("空文字の暗号化結果が空")
	}
}

// --- 新スキーマ（secret/environments）の YAML パーステスト ---

func TestVarConf_SecretEnvironmentsField(t *testing.T) {
	yamlText := `
defaults:
  secret: true
  environments: [production, preview]
variables:
  MY_SECRET:
    secret: true
  MY_VAR:
    secret: false
    environments: [development]
`
	var def config.Definition
	if err := yaml.Unmarshal([]byte(yamlText), &def); err != nil {
		t.Fatalf("YAML パース失敗: %v", err)
	}

	if def.Defaults.Secret == nil || !*def.Defaults.Secret {
		t.Errorf("defaults.secret = nil or false, want true")
	}
	if len(def.Defaults.Environments) != 2 {
		t.Errorf("defaults.environments len = %d, want 2", len(def.Defaults.Environments))
	}
	if def.Variables["MY_SECRET"].Secret == nil || !*def.Variables["MY_SECRET"].Secret {
		t.Errorf("MY_SECRET.secret = nil or false, want true")
	}
	if def.Variables["MY_VAR"].Secret == nil || *def.Variables["MY_VAR"].Secret {
		t.Errorf("MY_VAR.secret = nil or true, want false")
	}
	if len(def.Variables["MY_VAR"].Environments) != 1 || def.Variables["MY_VAR"].Environments[0] != "development" {
		t.Errorf("MY_VAR.environments = %v, want [development]", def.Variables["MY_VAR"].Environments)
	}
}

// --- expandGitHubTasks のテスト ---

func TestExpandGitHubTasks_EmptyEnvironments_RepoLevel(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: nil},
	}
	tasks := expandGitHubTasks(entries)
	if len(tasks) != 1 {
		t.Fatalf("tasks len = %d, want 1", len(tasks))
	}
	if tasks[0].envScope != "" {
		t.Errorf("envScope = %q, want empty (repo level)", tasks[0].envScope)
	}
	if tasks[0].entry.Key != "FOO" {
		t.Errorf("entry.Key = %q, want FOO", tasks[0].entry.Key)
	}
}

func TestExpandGitHubTasks_MultipleEnvironments(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: false, Environments: []string{"production", "staging"}},
	}
	tasks := expandGitHubTasks(entries)
	if len(tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(tasks))
	}
	if tasks[0].envScope != "production" {
		t.Errorf("tasks[0].envScope = %q, want production", tasks[0].envScope)
	}
	if tasks[1].envScope != "staging" {
		t.Errorf("tasks[1].envScope = %q, want staging", tasks[1].envScope)
	}
}

func TestExpandGitHubTasks_MixedEntries(t *testing.T) {
	entries := []provider.Entry{
		{Key: "REPO_SECRET", Value: "s1", Secret: true, Environments: nil},
		{Key: "ENV_VAR", Value: "v1", Secret: false, Environments: []string{"production", "preview"}},
	}
	tasks := expandGitHubTasks(entries)
	if len(tasks) != 3 {
		t.Fatalf("tasks len = %d, want 3", len(tasks))
	}
	if tasks[0].envScope != "" || tasks[0].entry.Key != "REPO_SECRET" {
		t.Errorf("tasks[0] = {%q, %q}, want {, REPO_SECRET}", tasks[0].envScope, tasks[0].entry.Key)
	}
	if tasks[1].envScope != "production" || tasks[1].entry.Key != "ENV_VAR" {
		t.Errorf("tasks[1] = {%q, %q}, want {production, ENV_VAR}", tasks[1].envScope, tasks[1].entry.Key)
	}
	if tasks[2].envScope != "preview" || tasks[2].entry.Key != "ENV_VAR" {
		t.Errorf("tasks[2] = {%q, %q}, want {preview, ENV_VAR}", tasks[2].envScope, tasks[2].entry.Key)
	}
}
