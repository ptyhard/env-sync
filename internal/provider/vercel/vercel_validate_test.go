package vercel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// withAPIBase はテスト中だけ apiBase をテストサーバに差し替える。
func withAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := apiBase
	apiBase = base
	t.Cleanup(func() { apiBase = orig })
}

// captureOsExit はテスト中に validateOsExit を差し替えてキャプチャする。
func captureOsExit(t *testing.T) *int {
	t.Helper()
	code := -1
	orig := validateOsExit
	validateOsExit = func(c int) { code = c }
	t.Cleanup(func() { validateOsExit = orig })
	return &code
}

// TestVercelCheckAccess_200 は vercelCheckAccess が 200 を返すことを確認する。
func TestVercelCheckAccess_200(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"envs":[]}`))
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	client := &http.Client{}
	status, _, err := vercelCheckAccess(client, "tok", "pid", "")
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

// TestVercelCheckAccess_404 は vercelCheckAccess が 404 を返すことを確認する。
func TestVercelCheckAccess_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET のみ許可
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Project not found."}}`))
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	client := &http.Client{}
	status, detail, err := vercelCheckAccess(client, "tok", "pid", "team1")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
	if detail == "" {
		t.Error("detail は空でないことを期待（エラーメッセージが含まれる）")
	}
}

// TestVercelCheckAccess_401 は vercelCheckAccess が 401 を返すことを確認する。
func TestVercelCheckAccess_401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	client := &http.Client{}
	status, _, err := vercelCheckAccess(client, "bad-tok", "pid", "")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", status)
	}
}

// TestVercelCheckAccess_403 は vercelCheckAccess が 403 を返すことを確認する。
func TestVercelCheckAccess_403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	client := &http.Client{}
	status, _, err := vercelCheckAccess(client, "tok", "pid", "")
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
}

// TestVercelValidate_GETOnly は Validate が GET のみを発行することを確認する。
func TestVercelValidate_GETOnly(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"envs":[]}`))
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	exitCode := captureOsExit(t)

	t.Setenv("VERCEL_TOKEN", "test-tok")
	t.Setenv("VERCEL_PROJECT_ID", "test-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	v := &vercelProvider{}
	opts := provider.Options{
		Env: ".env",
		Def: "env-sync.yaml",
	}
	_ = v.Validate(opts, nil)

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

// TestVercelValidate_404_ExitsOne は 404 のとき exit(1) が呼ばれることを確認する。
func TestVercelValidate_404_ExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"Project not found."}}`))
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	exitCode := captureOsExit(t)

	t.Setenv("VERCEL_TOKEN", "test-tok")
	t.Setenv("VERCEL_PROJECT_ID", "test-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	v := &vercelProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = v.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("404 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestVercelValidate_401_ExitsOne は 401 のとき exit(1) が呼ばれることを確認する。
func TestVercelValidate_401_ExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	exitCode := captureOsExit(t)

	t.Setenv("VERCEL_TOKEN", "test-tok")
	t.Setenv("VERCEL_PROJECT_ID", "test-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	v := &vercelProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = v.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("401 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestVercelValidate_TokenUnset_SkipsAPI は token 未設定のとき API 確認がスキップされることを確認する。
// API サーバが呼ばれないことを確認する。
func TestVercelValidate_TokenUnset_SkipsAPI(t *testing.T) {
	apiCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	exitCode := captureOsExit(t)

	t.Setenv("VERCEL_TOKEN", "") // token 未設定
	t.Setenv("VERCEL_PROJECT_ID", "test-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	v := &vercelProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = v.Validate(opts, nil)

	if apiCalled {
		t.Error("token 未設定のとき API が呼ばれてはいけない")
	}
	// token 未設定は失敗扱い
	if *exitCode != 1 {
		t.Errorf("token 未設定時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestVercelValidate_403_ExitsOne は 403 のとき exit(1) が呼ばれることを確認する。
func TestVercelValidate_403_ExitsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("GET 以外のメソッドが使用された: %s", r.Method)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	exitCode := captureOsExit(t)

	t.Setenv("VERCEL_TOKEN", "test-tok")
	t.Setenv("VERCEL_PROJECT_ID", "test-pid")
	t.Setenv("VERCEL_TEAM_ID", "")
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")

	v := &vercelProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = v.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("403 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
}

// TestVercelCheckAccess_RequestPathAndHeaders は vercelCheckAccess が
// GET /v10/projects/{id}/env に teamId クエリと Bearer トークンを付けて送ることを確認する。
func TestVercelCheckAccess_RequestPathAndHeaders(t *testing.T) {
	var gotPath, gotTeamID, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotTeamID = r.URL.Query().Get("teamId")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"envs":[]}`))
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	client := &http.Client{}
	if _, _, err := vercelCheckAccess(client, "secret-tok", "pid-123", "team-xyz"); err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}

	if gotPath != "/v10/projects/pid-123/env" {
		t.Errorf("リクエストパス = %q, want /v10/projects/pid-123/env", gotPath)
	}
	if gotTeamID != "team-xyz" {
		t.Errorf("teamId クエリ = %q, want team-xyz", gotTeamID)
	}
	if gotAuth != "Bearer secret-tok" {
		t.Errorf("Authorization ヘッダ = %q, want Bearer secret-tok", gotAuth)
	}
}

// TestVercelCheckAccess_NoTeamID_OmitsQuery は teamID が空のとき teamId クエリを付けないことを確認する。
func TestVercelCheckAccess_NoTeamID_OmitsQuery(t *testing.T) {
	var hasTeamID bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasTeamID = r.URL.Query()["teamId"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	client := &http.Client{}
	if _, _, err := vercelCheckAccess(client, "tok", "pid", ""); err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if hasTeamID {
		t.Error("teamID が空のとき teamId クエリは付与されてはいけない")
	}
}

// TestVercelCheckAccess_NetworkError は接続不可のとき err が返ることを確認する。
func TestVercelCheckAccess_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close() // 即座に閉じて接続不可にする
	withAPIBase(t, base)

	client := &http.Client{}
	_, _, err := vercelCheckAccess(client, "tok", "pid", "")
	if err == nil {
		t.Error("接続不可のとき err を期待したが nil")
	}
}

// TestVercelSourceLabel は取得元識別子が i18n ラベルに変換されることを確認する。
func TestVercelSourceLabel(t *testing.T) {
	cases := []struct {
		src  string
		want i18n.MsgKey
	}{
		{"env", i18n.MsgValidateSourceEnv},
		{"config", i18n.MsgValidateSourceConfig},
		{"project_json", i18n.MsgValidateSourceProjectJSON},
		{"git_remote", i18n.MsgValidateSourceGitRemote},
		{"", i18n.MsgValidateSourceUnset},
		{"unknown-source", i18n.MsgValidateSourceUnset},
	}
	for _, c := range cases {
		got := vercelSourceLabel(c.src)
		want := i18n.T(c.want)
		if got != want {
			t.Errorf("vercelSourceLabel(%q) = %q, want %q", c.src, got, want)
		}
	}
}

// TestVercelValidate_MixedTargets_ExitsOne は複数ターゲットで一部が NG のとき
// exit(1) が呼ばれ、出力に成功/失敗の集計が含まれることを確認する。
func TestVercelValidate_MixedTargets_ExitsOne(t *testing.T) {
	// project_id を URL から判定して app-a は 200、app-b は 404 を返す
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pid-b") {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"message":"Project not found."}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"envs":[]}`))
	}))
	defer srv.Close()
	withAPIBase(t, srv.URL)

	exitCode := captureOsExit(t)

	// 出力をキャプチャ
	var buf strings.Builder
	origOut := stdoutWriter
	stdoutWriter = &buf
	t.Cleanup(func() { stdoutWriter = origOut })

	t.Setenv("VERCEL_TOKEN", "")
	t.Setenv("VERCEL_PROJECT_ID", "")
	t.Setenv("VERCEL_TEAM_ID", "")

	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir+"/no-global")
	if err := os.WriteFile(dir+"/.env-sync.config.yaml", []byte(`
vercel:
  token: cfg-tok
  projects:
    - name: app-a
      project_id: pid-a
    - name: app-b
      project_id: pid-b
`), 0600); err != nil {
		t.Fatal(err)
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	v := &vercelProvider{}
	opts := provider.Options{Env: ".env", Def: "env-sync.yaml"}
	_ = v.Validate(opts, nil)

	if *exitCode != 1 {
		t.Errorf("一部 NG 時は exit(1) を期待, exitCode = %d", *exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "app-a") || !strings.Contains(out, "app-b") {
		t.Errorf("出力に両ターゲットのラベルが含まれることを期待: %q", out)
	}
}

// TestApplyProjectJSONFallback_NoFile は .vercel/project.json が存在しない場合に何もしないことを確認する。
func TestApplyProjectJSONFallback_NoFile(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	targets := []config.VercelTarget{{ProjectID: ""}}
	used, err := applyProjectJSONFallback(targets)
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if used {
		t.Error("project.json が存在しないので used=false を期待")
	}
	if targets[0].ProjectID != "" {
		t.Errorf("ProjectID は空のまま: %q", targets[0].ProjectID)
	}
}

// TestApplyProjectJSONFallback_WithFile は .vercel/project.json が存在する場合に ProjectID が設定されることを確認する。
func TestApplyProjectJSONFallback_WithFile(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// .vercel/project.json を作成
	if err := os.MkdirAll(dir+"/.vercel", 0700); err != nil {
		t.Fatal(err)
	}
	jsonContent := `{"projectId":"pj-from-file","orgId":"org-from-file"}`
	if err := os.WriteFile(dir+"/.vercel/project.json", []byte(jsonContent), 0600); err != nil {
		t.Fatal(err)
	}

	targets := []config.VercelTarget{{ProjectID: ""}}
	used, err := applyProjectJSONFallback(targets)
	if err != nil {
		t.Fatalf("エラーなしを期待: %v", err)
	}
	if !used {
		t.Error("project.json が存在するので used=true を期待")
	}
	if targets[0].ProjectID != "pj-from-file" {
		t.Errorf("ProjectID = %q, want pj-from-file", targets[0].ProjectID)
	}
	if targets[0].TeamID != "org-from-file" {
		t.Errorf("TeamID = %q, want org-from-file", targets[0].TeamID)
	}
	if targets[0].ProjectIDSource != "project_json" {
		t.Errorf("ProjectIDSource = %q, want project_json", targets[0].ProjectIDSource)
	}
}
