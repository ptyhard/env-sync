package cloudflare

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/provider"
)

// withAPIBase はテスト中だけ apiBase をテストサーバに差し替える。
func withAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := apiBase
	apiBase = base
	t.Cleanup(func() { apiBase = orig })
}

// recordedRequest はテストサーバが受け取ったリクエストの記録。
type recordedRequest struct {
	method string
	path   string
	auth   string
	body   string
}

// newRecordingServer はリクエストを記録し、handler の応答を返すテストサーバを立てる。
// handler が nil の場合は success:true の空レスポンスを返す。
func newRecordingServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var got []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			body:   string(body),
		})
		mu.Unlock()
		if handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"success":true,"errors":[],"messages":[],"result":{}}`) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)
	withAPIBase(t, srv.URL)
	return srv, &got
}

// --- syncOneTarget のテスト ---

func TestSyncOneTarget_SendsSecretTextPut(t *testing.T) {
	_, got := newRecordingServer(t, nil)

	tasks := []cloudflareTask{
		{script: "my-worker", entry: provider.Entry{Key: "API_KEY", Value: "s3cret", Secret: true}},
	}
	ok, ng := syncOneTarget(&http.Client{}, "tok", "acct1", tasks)
	if ok != 1 || ng != 0 {
		t.Fatalf("(ok, ng) = (%d, %d), want (1, 0)", ok, ng)
	}
	if len(*got) != 1 {
		t.Fatalf("リクエスト数 = %d, want 1", len(*got))
	}
	req := (*got)[0]
	if req.method != http.MethodPut {
		t.Errorf("method = %s, want PUT", req.method)
	}
	if want := "/accounts/acct1/workers/scripts/my-worker/secrets"; req.path != want {
		t.Errorf("path = %s, want %s", req.path, want)
	}
	if req.auth != "Bearer tok" {
		t.Errorf("Authorization = %q, want %q", req.auth, "Bearer tok")
	}
	var item secretItem
	if err := json.Unmarshal([]byte(req.body), &item); err != nil {
		t.Fatalf("リクエストボディが JSON でない: %v", err)
	}
	if item.Name != "API_KEY" || item.Text != "s3cret" || item.Type != "secret_text" {
		t.Errorf("ボディ = %+v, want {API_KEY s3cret secret_text}", item)
	}
}

func TestSyncOneTarget_SendsToPerEnvironmentScripts(t *testing.T) {
	_, got := newRecordingServer(t, nil)

	tasks := expandCloudflareTasks(
		[]provider.Entry{{Key: "DB_URL", Value: "v", Secret: true, Environments: []string{"production", "staging"}}},
		"my-worker", nil)
	ok, ng := syncOneTarget(&http.Client{}, "tok", "acct1", tasks)
	if ok != 2 || ng != 0 {
		t.Fatalf("(ok, ng) = (%d, %d), want (2, 0)", ok, ng)
	}
	wantPaths := []string{
		"/accounts/acct1/workers/scripts/my-worker-production/secrets",
		"/accounts/acct1/workers/scripts/my-worker-staging/secrets",
	}
	for i, want := range wantPaths {
		if (*got)[i].path != want {
			t.Errorf("リクエスト[%d] path = %s, want %s", i, (*got)[i].path, want)
		}
	}
}

func TestSyncOneTarget_CountsHTTPErrorAsFailure(t *testing.T) {
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"success":false,"errors":[{"code":10007,"message":"script not found"}]}`) //nolint:errcheck
	})

	tasks := []cloudflareTask{{script: "w", entry: provider.Entry{Key: "K", Value: "v"}}}
	ok, ng := syncOneTarget(&http.Client{}, "tok", "acct1", tasks)
	if ok != 0 || ng != 1 {
		t.Errorf("(ok, ng) = (%d, %d), want (0, 1)", ok, ng)
	}
}

func TestSyncOneTarget_CountsSuccessFalseAsFailure(t *testing.T) {
	// Cloudflare は HTTP 200 でも success:false を返すことがある
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":false,"errors":[{"message":"invalid binding"}]}`) //nolint:errcheck
	})

	tasks := []cloudflareTask{{script: "w", entry: provider.Entry{Key: "K", Value: "v"}}}
	ok, ng := syncOneTarget(&http.Client{}, "tok", "acct1", tasks)
	if ok != 0 || ng != 1 {
		t.Errorf("(ok, ng) = (%d, %d), want (0, 1)", ok, ng)
	}
}

// --- fetchSecretNames のテスト ---

func TestFetchSecretNames_ReturnsNamesPerScript(t *testing.T) {
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "my-worker-staging") {
			io.WriteString(w, `{"success":true,"result":[{"name":"STG_ONLY","type":"secret_text"}]}`) //nolint:errcheck
			return
		}
		io.WriteString(w, `{"success":true,"result":[{"name":"API_KEY","type":"secret_text"},{"name":"DB_URL","type":"secret_text"}]}`) //nolint:errcheck
	})

	got, err := fetchSecretNames(&http.Client{}, "tok", "acct1", []string{"my-worker", "my-worker-staging"})
	if err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if len(got["my-worker"]) != 2 || got["my-worker"][0] != "API_KEY" {
		t.Errorf("my-worker のシークレット = %v, want [API_KEY DB_URL]", got["my-worker"])
	}
	if len(got["my-worker-staging"]) != 1 || got["my-worker-staging"][0] != "STG_ONLY" {
		t.Errorf("my-worker-staging のシークレット = %v, want [STG_ONLY]", got["my-worker-staging"])
	}
}

func TestFetchSecretNames_ErrorOnHTTPFailure(t *testing.T) {
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"success":false,"errors":[{"message":"insufficient permissions"}]}`) //nolint:errcheck
	})

	_, err := fetchSecretNames(&http.Client{}, "tok", "acct1", []string{"w"})
	if err == nil {
		t.Fatal("エラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "insufficient permissions") {
		t.Errorf("エラーメッセージにレスポンス詳細が含まれない: %v", err)
	}
}

func TestFetchSecretNames_ErrorOnSuccessFalse(t *testing.T) {
	// HTTP 200 かつ success:false は失敗として扱う（部分的な結果で prune 判定しないため）
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":false,"errors":[{"message":"nope"}],"result":null}`) //nolint:errcheck
	})

	if _, err := fetchSecretNames(&http.Client{}, "tok", "acct1", []string{"w"}); err == nil {
		t.Fatal("success:false でエラーを期待したが nil")
	}
}

// --- deleteSecrets のテスト ---

func TestDeleteSecrets_SendsDeleteWithEscapedName(t *testing.T) {
	_, got := newRecordingServer(t, nil)

	targets := []pruneTarget{{script: "my-worker", name: "STALE_KEY"}}
	ok, ng := deleteSecrets(&http.Client{}, "tok", "acct1", targets)
	if ok != 1 || ng != 0 {
		t.Fatalf("(ok, ng) = (%d, %d), want (1, 0)", ok, ng)
	}
	req := (*got)[0]
	if req.method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", req.method)
	}
	if want := "/accounts/acct1/workers/scripts/my-worker/secrets/STALE_KEY"; req.path != want {
		t.Errorf("path = %s, want %s", req.path, want)
	}
}

// --- checkResponse のテスト ---

func TestCheckResponse(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErrMsg string // "" なら成功扱いを期待
	}{
		{"2xx かつ success:true は成功", 200, `{"success":true,"result":{}}`, ""},
		{"2xx で success フィールドが無ければ成功", 200, `{}`, ""},
		{"2xx でボディが JSON でなくても成功", 200, `OK`, ""},
		{"2xx でも success:false は失敗", 200, `{"success":false,"errors":[{"message":"bad"}]}`, "bad"},
		{"4xx は失敗", 404, `{"success":false,"errors":[{"message":"not found"}]}`, "HTTP 404: not found"},
		{"4xx でボディが空なら status のみ", 500, ``, "HTTP 500"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body) //nolint:errcheck
			}))
			t.Cleanup(srv.Close)

			res, err := http.Get(srv.URL) //nolint:noctx
			if err != nil {
				t.Fatalf("リクエスト失敗: %v", err)
			}
			defer res.Body.Close()

			got := checkResponse(res)
			if got != tt.wantErrMsg {
				t.Errorf("checkResponse() = %q, want %q", got, tt.wantErrMsg)
			}
		})
	}
}

// --- applyWranglerFallback のテスト ---

func TestApplyWranglerFallback_ReadsTomlName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wrangler.toml"), "name = \"toml-worker\"\n")
	t.Chdir(dir)

	targets := []config.CloudflareTarget{{}}
	if err := applyWranglerFallback(targets); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if targets[0].Script != "toml-worker" {
		t.Errorf("Script = %q, want %q", targets[0].Script, "toml-worker")
	}
	if targets[0].ScriptSource != "wrangler" {
		t.Errorf("ScriptSource = %q, want %q", targets[0].ScriptSource, "wrangler")
	}
}

func TestApplyWranglerFallback_PrefersJsoncOverToml(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wrangler.jsonc"), "{\n // name\n \"name\": \"jsonc-worker\"\n}")
	writeFile(t, filepath.Join(dir, "wrangler.toml"), "name = \"toml-worker\"\n")
	t.Chdir(dir)

	targets := []config.CloudflareTarget{{}}
	if err := applyWranglerFallback(targets); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if targets[0].Script != "jsonc-worker" {
		t.Errorf("Script = %q, want %q（jsonc を優先）", targets[0].Script, "jsonc-worker")
	}
}

func TestApplyWranglerFallback_DoesNotOverrideExistingScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wrangler.toml"), "name = \"toml-worker\"\n")
	t.Chdir(dir)

	targets := []config.CloudflareTarget{{Script: "explicit", ScriptSource: "config"}}
	if err := applyWranglerFallback(targets); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if targets[0].Script != "explicit" {
		t.Errorf("Script = %q, want %q（既存値を上書きしない）", targets[0].Script, "explicit")
	}
}

func TestApplyWranglerFallback_SkipsWhenMultipleTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wrangler.toml"), "name = \"toml-worker\"\n")
	t.Chdir(dir)

	targets := []config.CloudflareTarget{{}, {}}
	if err := applyWranglerFallback(targets); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if targets[0].Script != "" {
		t.Errorf("Script = %q, want \"\"（複数ターゲット時はフォールバックしない）", targets[0].Script)
	}
}

func TestApplyWranglerFallback_NoConfigFileIsNotError(t *testing.T) {
	t.Chdir(t.TempDir())

	targets := []config.CloudflareTarget{{}}
	if err := applyWranglerFallback(targets); err != nil {
		t.Fatalf("wrangler 設定が無い場合はエラーにしない: %v", err)
	}
	if targets[0].Script != "" {
		t.Errorf("Script = %q, want \"\"", targets[0].Script)
	}
}

func TestApplyWranglerFallback_FallsThroughWhenNameIsMissing(t *testing.T) {
	// name フィールドが無い設定ファイルは次の候補へフォールスルーする
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "wrangler.json"), `{"main":"src/index.ts"}`)
	writeFile(t, filepath.Join(dir, "wrangler.toml"), "name = \"toml-worker\"\n")
	t.Chdir(dir)

	targets := []config.CloudflareTarget{{}}
	if err := applyWranglerFallback(targets); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if targets[0].Script != "toml-worker" {
		t.Errorf("Script = %q, want %q", targets[0].Script, "toml-worker")
	}
}

// writeFile はテスト用にファイルを書き出すヘルパー。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("ファイル書き込み失敗: %v", err)
	}
}

// --- Sync のエンドツーエンドテスト ---

func TestSync_SkipsPlainVarsAndSyncsSecrets(t *testing.T) {
	isolateConfig(t)
	_, requests := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
			return
		}
		io.WriteString(w, `{"success":true,"result":{}}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	entries := []provider.Entry{
		{Key: "API_KEY", Value: "s1", Secret: true},
		{Key: "PUBLIC_URL", Value: "https://example.com", Secret: false},
	}
	p := &cloudflareProvider{}
	if err := p.Sync(provider.Options{Yes: true, Env: ".env", Def: "env-sync.yaml"}, entries); err != nil {
		t.Fatalf("エラー: %v", err)
	}

	var puts []recordedRequest
	for _, req := range *requests {
		if req.method == http.MethodPut {
			puts = append(puts, req)
		}
	}
	if len(puts) != 1 {
		t.Fatalf("PUT 数 = %d, want 1（secret=false はスキップされるはず）", len(puts))
	}
	if !strings.Contains(puts[0].body, "API_KEY") {
		t.Errorf("送信されたのが API_KEY でない: %s", puts[0].body)
	}
	if strings.Contains(puts[0].body, "PUBLIC_URL") {
		t.Error("secret=false の PUBLIC_URL が送信されている")
	}
}

func TestSync_PrunesUndefinedSecrets(t *testing.T) {
	isolateConfig(t)
	_, requests := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"success":true,"result":[{"name":"API_KEY"},{"name":"STALE_KEY"}]}`) //nolint:errcheck
			return
		}
		io.WriteString(w, `{"success":true,"result":{}}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	opts := provider.Options{Yes: true, Prune: true, DefinedKeys: []string{"API_KEY"}}
	p := &cloudflareProvider{}
	if err := p.Sync(opts, entries); err != nil {
		t.Fatalf("エラー: %v", err)
	}

	var deletes []recordedRequest
	for _, req := range *requests {
		if req.method == http.MethodDelete {
			deletes = append(deletes, req)
		}
	}
	if len(deletes) != 1 {
		t.Fatalf("DELETE 数 = %d, want 1", len(deletes))
	}
	if !strings.HasSuffix(deletes[0].path, "/secrets/STALE_KEY") {
		t.Errorf("削除対象 = %s, want STALE_KEY", deletes[0].path)
	}
}

func TestSync_DryRunDoesNotWrite(t *testing.T) {
	isolateConfig(t)
	_, requests := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	p := &cloudflareProvider{}
	if err := p.Sync(provider.Options{DryRun: true}, entries); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	for _, req := range *requests {
		if req.method != http.MethodGet {
			t.Errorf("dry-run が %s リクエストを送信した: %s", req.method, req.path)
		}
	}
}

func TestSync_ErrorWhenScriptUnresolvable(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, nil)
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	p := &cloudflareProvider{}
	err := p.Sync(provider.Options{Yes: true}, entries)
	if err == nil {
		t.Fatal("script 未解決でエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "script name could not be resolved") {
		t.Errorf("エラーメッセージが想定と異なる: %v", err)
	}
}

func TestSync_ErrorWhenTokenMissing(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, nil)
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	p := &cloudflareProvider{}
	if err := p.Sync(provider.Options{Yes: true}, entries); err == nil {
		t.Fatal("トークン未設定でエラーを期待したが nil")
	}
}

// captureSyncOsExit はテスト中に syncOsExit を差し替えて終了コードをキャプチャする。
func captureSyncOsExit(t *testing.T) *int {
	t.Helper()
	code := 0
	orig := syncOsExit
	syncOsExit = func(c int) { code = c }
	t.Cleanup(func() { syncOsExit = orig })
	return &code
}

func TestSync_CountsTokenMissingTargetAsFailure(t *testing.T) {
	// 複数ターゲットのうち 1 つがトークン未設定でスキップされた場合、
	// そのターゲットへ送るはずだった件数を失敗として集計し、終了コード 1 にする。
	// （集計しないと、他ターゲットが成功しただけで exit 0 になり同期漏れを見逃す）
	isolateConfig(t)
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
			return
		}
		io.WriteString(w, `{"success":true,"result":{}}`) //nolint:errcheck
	})
	// api はトークンあり、cron はトークンなし（top-level / 環境変数にもトークンを置かない）
	writeFile(t, ".env-sync.config.yaml", `cloudflare:
  account_id: acct1
  scripts:
    - name: api
      script: my-api-worker
      api_token: tok
    - name: cron
      script: my-cron-worker
`)

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	code := captureSyncOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Sync(provider.Options{Yes: true}, entries); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if *code != 1 {
		t.Errorf("終了コード = %d, want 1（トークン未設定でスキップしたターゲットを失敗として集計するはず）", *code)
	}
}

func TestSync_ExitsZeroWhenAllTargetsSucceed(t *testing.T) {
	// 全ターゲットにトークンがある場合は失敗 0 件で終了コードを変えない
	isolateConfig(t)
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
			return
		}
		io.WriteString(w, `{"success":true,"result":{}}`) //nolint:errcheck
	})
	writeFile(t, ".env-sync.config.yaml", `cloudflare:
  api_token: tok
  account_id: acct1
  scripts:
    - name: api
      script: my-api-worker
    - name: cron
      script: my-cron-worker
`)

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	code := captureSyncOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Sync(provider.Options{Yes: true}, entries); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if *code != 0 {
		t.Errorf("終了コード = %d, want 0", *code)
	}
}

func TestSync_UsesWranglerFallbackForScriptName(t *testing.T) {
	isolateConfig(t)
	writeFile(t, "wrangler.toml", "name = \"fallback-worker\"\n")
	_, requests := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
			return
		}
		io.WriteString(w, `{"success":true,"result":{}}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")

	entries := []provider.Entry{{Key: "API_KEY", Value: "s1", Secret: true}}
	p := &cloudflareProvider{}
	if err := p.Sync(provider.Options{Yes: true}, entries); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	found := false
	for _, req := range *requests {
		if strings.Contains(req.path, "/scripts/fallback-worker/") {
			found = true
		}
	}
	if !found {
		t.Errorf("wrangler.toml の name が使われていない: %+v", *requests)
	}
}
