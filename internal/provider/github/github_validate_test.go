package github

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// captureGitHubOsExit はテスト中に githubValidateOsExit を差し替えてキャプチャする。
func captureGitHubOsExit(t *testing.T) *int {
	t.Helper()
	code := -1
	orig := githubValidateOsExit
	githubValidateOsExit = func(c int) { code = c }
	t.Cleanup(func() { githubValidateOsExit = orig })
	return &code
}

// TestGitHubCheckAccess_200 は githubCheckAccess が 200 を返すことを確認する。
func TestGitHubCheckAccess_200(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo"}`))
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	client := &http.Client{}
	status, err := githubCheckAccess(client, "tok", "owner", "repo")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	// GET のみ使用されることを確認
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", m)
		}
	}
}

// TestGitHubCheckAccess_404 は githubCheckAccess が 404 を返すことを確認する。
func TestGitHubCheckAccess_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	client := &http.Client{}
	status, err := githubCheckAccess(client, "tok", "owner", "repo")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
}

// TestGitHubCheckAccess_401 は githubCheckAccess が 401 を返すことを確認する。
func TestGitHubCheckAccess_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	client := &http.Client{}
	status, err := githubCheckAccess(client, "bad-tok", "owner", "repo")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

// TestGitHubCheckAccess_403 は githubCheckAccess が 403 を返すことを確認する。
func TestGitHubCheckAccess_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	client := &http.Client{}
	status, err := githubCheckAccess(client, "tok", "owner", "repo")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
}

// TestGitHubValidate_GETOnly は Validate が GET のみを発行することを確認する。
func TestGitHubValidate_GETOnly(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo"}`))
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	exitCode := captureGitHubOsExit(t)

	t.Setenv("GITHUB_TOKEN", "test-tok")
	t.Setenv("GITHUB_REPO", "owner/repo")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	g := &githubProvider{}
	opts := provider.Options{
		Env: ".env",
		Def: "env-sync.yaml",
	}
	_ = g.Validate(opts, nil)

	// GET のみ使用されることを確認
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", m)
		}
	}
	// 200 で成功した場合 exit は呼ばれない
	if *exitCode != -1 {
		t.Errorf("成功時は exit 不要, exitCode = %d", *exitCode)
	}
}

// TestGitHubValidate_404_ExitsOne は 404 のとき exit(1) が呼ばれることを確認する。
func TestGitHubValidate_404_ExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	exitCode := captureGitHubOsExit(t)

	t.Setenv("GITHUB_TOKEN", "test-tok")
	t.Setenv("GITHUB_REPO", "owner/repo")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	g := &githubProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = g.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("404 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestGitHubValidate_401_ExitsOne は 401 のとき exit(1) が呼ばれることを確認する。
func TestGitHubValidate_401_ExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	exitCode := captureGitHubOsExit(t)

	t.Setenv("GITHUB_TOKEN", "bad-tok")
	t.Setenv("GITHUB_REPO", "owner/repo")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	g := &githubProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = g.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("401 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestGitHubValidate_403_ExitsOne は 403 のとき exit(1) が呼ばれることを確認する。
func TestGitHubValidate_403_ExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	exitCode := captureGitHubOsExit(t)

	t.Setenv("GITHUB_TOKEN", "test-tok")
	t.Setenv("GITHUB_REPO", "owner/repo")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	g := &githubProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = g.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("403 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestGitHubCheckAccess_RequestPathAndHeaders は githubCheckAccess が
// GET /repos/{owner}/{repo} に認証・API バージョンヘッダを付けて送ることを確認する。
func TestGitHubCheckAccess_RequestPathAndHeaders(t *testing.T) {
	var gotPath, gotAuth, gotAccept, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo"}`))
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	client := &http.Client{}
	if _, err := githubCheckAccess(client, "secret-tok", "owner", "repo"); err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}

	if gotPath != "/repos/owner/repo" {
		t.Errorf("リクエストパス = %q, want /repos/owner/repo", gotPath)
	}
	if gotAuth != "Bearer secret-tok" {
		t.Errorf("Authorization ヘッダ = %q, want Bearer secret-tok", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept ヘッダ = %q, want application/vnd.github+json", gotAccept)
	}
	if gotVersion != "2022-11-28" {
		t.Errorf("X-GitHub-Api-Version ヘッダ = %q, want 2022-11-28", gotVersion)
	}
}

// TestGitHubCheckAccess_NetworkError は接続不可のとき err が返ることを確認する。
func TestGitHubCheckAccess_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close() // 即座に閉じて接続不可にする
	withGitHubAPIBase(t, base)

	client := &http.Client{}
	_, err := githubCheckAccess(client, "tok", "owner", "repo")
	if err == nil {
		t.Error("接続不可のとき err を期待したが nil")
	}
}

// TestGitHubValidate_RepoUnresolved_SkipsAPI は repo を解決できない場合に
// API 確認をスキップして exit(1) になることを確認する。
// GITHUB_REPO 未設定・config なし・git remote も取得できない一時ディレクトリで実行する。
func TestGitHubValidate_RepoUnresolved_SkipsAPI(t *testing.T) {
	apiCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	exitCode := captureGitHubOsExit(t)

	t.Setenv("GITHUB_TOKEN", "test-tok")
	t.Setenv("GITHUB_REPO", "") // repo 未設定
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	// git remote が取得できない一時ディレクトリへ移動（リポジトリ外）
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	g := &githubProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = g.Validate(opts, nil)

	if apiCalled {
		t.Error("repo 未解決のとき API が呼ばれてはいけない")
	}
	if *exitCode != 1 {
		t.Errorf("repo 未解決時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestGitHubSourceLabel は取得元識別子が i18n ラベルに変換されることを確認する。
func TestGitHubSourceLabel(t *testing.T) {
	cases := []struct {
		src  string
		want i18n.MsgKey
	}{
		{"env", i18n.MsgValidateSourceEnv},
		{"config", i18n.MsgValidateSourceConfig},
		{"git_remote", i18n.MsgValidateSourceGitRemote},
		{"", i18n.MsgValidateSourceUnset},
		{"unknown-source", i18n.MsgValidateSourceUnset},
	}
	for _, c := range cases {
		got := githubSourceLabel(c.src)
		want := i18n.T(c.want)
		if got != want {
			t.Errorf("githubSourceLabel(%q) = %q, want %q", c.src, got, want)
		}
	}
}

// TestGitHubValidate_TokenUnset_SkipsAPI は token 未設定のとき API 確認がスキップされることを確認する。
func TestGitHubValidate_TokenUnset_SkipsAPI(t *testing.T) {
	apiCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	exitCode := captureGitHubOsExit(t)

	t.Setenv("GITHUB_TOKEN", "") // token 未設定
	t.Setenv("GITHUB_REPO", "owner/repo")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	g := &githubProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = g.Validate(opts, nil)

	if apiCalled {
		t.Error("token 未設定のとき API が呼ばれてはいけない")
	}
	if *exitCode != 1 {
		t.Errorf("token 未設定時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}
