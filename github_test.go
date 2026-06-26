package main

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"golang.org/x/crypto/nacl/box"
	"gopkg.in/yaml.v3"
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

// --- parseFlags の provider / github-env 解析テスト ---

func TestParseFlags_DefaultProvider(t *testing.T) {
	opts := parseFlags([]string{})
	if opts.provider != "vercel" {
		t.Errorf("provider のデフォルト = %q, want %q", opts.provider, "vercel")
	}
}

func TestParseFlags_ProviderVercel(t *testing.T) {
	opts := parseFlags([]string{"--provider", "vercel"})
	if opts.provider != "vercel" {
		t.Errorf("provider = %q, want vercel", opts.provider)
	}
}

func TestParseFlags_ProviderGitHub(t *testing.T) {
	opts := parseFlags([]string{"--provider", "github"})
	if opts.provider != "github" {
		t.Errorf("provider = %q, want github", opts.provider)
	}
}

func TestParseFlags_ProviderEqualForm(t *testing.T) {
	opts := parseFlags([]string{"--provider=github"})
	if opts.provider != "github" {
		t.Errorf("--provider=github が解析されない: got %q", opts.provider)
	}
}

func TestParseFlags_GithubEnvFlag(t *testing.T) {
	opts := parseFlags([]string{"--github-env", "production"})
	if opts.githubEnv != "production" {
		t.Errorf("--github-env = %q, want production", opts.githubEnv)
	}
}

func TestParseFlags_GithubEnvEqualForm(t *testing.T) {
	opts := parseFlags([]string{"--github-env=staging"})
	if opts.githubEnv != "staging" {
		t.Errorf("--github-env=staging が解析されない: got %q", opts.githubEnv)
	}
}

func TestParseFlags_DefaultGithubEnv(t *testing.T) {
	opts := parseFlags([]string{})
	if opts.githubEnv != "" {
		t.Errorf("githubEnv のデフォルト = %q, want empty", opts.githubEnv)
	}
}

func TestParseFlags_CombinedWithExistingFlags(t *testing.T) {
	opts := parseFlags([]string{"--provider", "github", "--github-env", "production", "--dry-run", "--yes"})
	if opts.provider != "github" {
		t.Errorf("provider = %q, want github", opts.provider)
	}
	if opts.githubEnv != "production" {
		t.Errorf("githubEnv = %q, want production", opts.githubEnv)
	}
	if !opts.dryRun {
		t.Error("dryRun が false")
	}
	if !opts.yes {
		t.Error("yes が false")
	}
}

// --- varConf / definition の Kind フィールドテスト ---

func TestVarConf_KindField(t *testing.T) {
	yamlText := `
defaults:
  kind: secret
variables:
  MY_SECRET:
    kind: secret
  MY_VAR:
    kind: variable
`
	var def definition
	if err := yaml.Unmarshal([]byte(yamlText), &def); err != nil {
		t.Fatalf("YAML パース失敗: %v", err)
	}

	if def.Defaults.Kind != "secret" {
		t.Errorf("defaults.kind = %q, want secret", def.Defaults.Kind)
	}
	if def.Variables["MY_SECRET"].Kind != "secret" {
		t.Errorf("MY_SECRET.kind = %q, want secret", def.Variables["MY_SECRET"].Kind)
	}
	if def.Variables["MY_VAR"].Kind != "variable" {
		t.Errorf("MY_VAR.kind = %q, want variable", def.Variables["MY_VAR"].Kind)
	}
}
