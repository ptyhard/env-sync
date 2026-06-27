package vercel

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ptyhard/env-sync/internal/config"
	"github.com/ptyhard/env-sync/internal/provider"
)

// item の JSON マーシャル形式テスト（key/value/type/target フィールド名）

func TestItem_JSONMarshal(t *testing.T) {
	it := item{
		Key:    "MY_KEY",
		Value:  "my-value",
		Type:   "sensitive",
		Target: []string{"production", "preview"},
	}

	data, err := json.Marshal(it)
	if err != nil {
		t.Fatalf("json.Marshal 失敗: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal 失敗: %v", err)
	}

	cases := []struct {
		field string
		want  interface{}
	}{
		{"key", "MY_KEY"},
		{"value", "my-value"},
		{"type", "sensitive"},
	}
	for _, tc := range cases {
		got, ok := m[tc.field]
		if !ok {
			t.Errorf("フィールド %q が JSON に存在しない", tc.field)
			continue
		}
		if got != tc.want {
			t.Errorf("m[%q] = %v, want %v", tc.field, got, tc.want)
		}
	}

	targets, ok := m["target"].([]interface{})
	if !ok {
		t.Fatal("target フィールドが配列でない")
	}
	if len(targets) != 2 || targets[0] != "production" || targets[1] != "preview" {
		t.Errorf("target = %v, want [production preview]", targets)
	}
}

func TestItem_JSONFieldNames(t *testing.T) {
	// JSON フィールド名が小文字であることを確認（Vercel API 要件）
	it := item{Key: "K", Value: "V", Type: "plain", Target: []string{"production"}}
	data, _ := json.Marshal(it)
	raw := string(data)

	for _, field := range []string{"key", "value", "type", "target"} {
		if !strings.Contains(raw, `"`+field+`"`) {
			t.Errorf("JSON に小文字フィールド %q が含まれない: %s", field, raw)
		}
	}
}

// parseErrorBody のテーブルテスト

func TestParseErrorBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "message あり",
			body: `{"error":{"message":"project not found","code":"not_found"}}`,
			want: "project not found",
		},
		{
			name: "message なし code あり",
			body: `{"error":{"code":"unauthorized"}}`,
			want: "unauthorized",
		},
		{
			name: "error フィールドなし",
			body: `{"foo":"bar"}`,
			want: "unknown error",
		},
		{
			name: "不正な JSON",
			body: `not json`,
			want: "",
		},
		{
			name: "空の error オブジェクト",
			body: `{"error":{}}`,
			want: "unknown error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseErrorBody(io.NopCloser(strings.NewReader(tc.body)))
			if got != tc.want {
				t.Errorf("parseErrorBody = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- entriesToVercelItems のテスト ---

func TestEntriesToVercelItems_SecretTrue_TypeSensitive(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: []string{"production"}},
	}
	items, err := entriesToVercelItems(entries)
	if err != nil {
		t.Fatalf("entriesToVercelItems エラー: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Type != "sensitive" {
		t.Errorf("Type = %q, want sensitive", items[0].Type)
	}
}

func TestEntriesToVercelItems_SecretFalse_TypePlain(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: false, Environments: []string{"production"}},
	}
	items, err := entriesToVercelItems(entries)
	if err != nil {
		t.Fatalf("entriesToVercelItems エラー: %v", err)
	}
	if items[0].Type != "plain" {
		t.Errorf("Type = %q, want plain", items[0].Type)
	}
}

func TestEntriesToVercelItems_EmptyEnvironments_DefaultTarget(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: nil},
	}
	items, err := entriesToVercelItems(entries)
	if err != nil {
		t.Fatalf("entriesToVercelItems エラー: %v", err)
	}
	if len(items[0].Target) != 2 {
		t.Fatalf("Target len = %d, want 2", len(items[0].Target))
	}
	if items[0].Target[0] != "production" || items[0].Target[1] != "preview" {
		t.Errorf("Target = %v, want [production preview]", items[0].Target)
	}
}

func TestEntriesToVercelItems_InvalidEnvironment_Error(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: []string{"staging"}},
	}
	_, err := entriesToVercelItems(entries)
	if err == nil {
		t.Fatal("不正な environments でエラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("エラーメッセージに staging が含まれていない: %v", err)
	}
}

func TestEntriesToVercelItems_ValidEnvironments(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: false, Environments: []string{"production", "preview", "development"}},
	}
	items, err := entriesToVercelItems(entries)
	if err != nil {
		t.Fatalf("entriesToVercelItems エラー: %v", err)
	}
	if len(items[0].Target) != 3 {
		t.Errorf("Target len = %d, want 3", len(items[0].Target))
	}
}

// --- syncOneVercelTarget のテスト ---

// withVercelAPIBase はテスト中だけ apiBase をテストサーバに差し替える。
func withVercelAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := apiBase
	apiBase = base
	t.Cleanup(func() { apiBase = orig })
}

// makeTestItems は テスト用に Entry スライスを item スライスへ変換するヘルパー。
func makeTestItems(t *testing.T, entries []provider.Entry) []item {
	t.Helper()
	items, err := entriesToVercelItems(entries)
	if err != nil {
		t.Fatalf("entriesToVercelItems: %v", err)
	}
	return items
}

func TestSyncOneVercelTarget_SendsToCorrectURL(t *testing.T) {
	var receivedPath string
	srv := newVercelTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusCreated)
	})
	withVercelAPIBase(t, srv.URL)

	items := makeTestItems(t, []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: []string{"production"}},
	})
	ok, ng := syncOneVercelTarget(
		&http.Client{},
		"test-token",
		"my-project-id",
		"", // teamID
		items,
	)
	if ok != 1 || ng != 0 {
		t.Errorf("ok=%d ng=%d, want ok=1 ng=0", ok, ng)
	}
	if receivedPath != "/v10/projects/my-project-id/env" {
		t.Errorf("path = %q, want /v10/projects/my-project-id/env", receivedPath)
	}
}

func TestSyncOneVercelTarget_MultipleProjects(t *testing.T) {
	// 2 つのプロジェクトに送信されることを確認
	var receivedPaths []string
	mu := &struct{ sync.Mutex }{}
	srv := newVercelTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			receivedPaths = append(receivedPaths, r.URL.Path)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		} else {
			// GET（既存 key 取得）には空レスポンスを返す
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"envs": []interface{}{}})
		}
	})
	withVercelAPIBase(t, srv.URL)

	items := makeTestItems(t, []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: []string{"production"}},
	})

	// プロジェクト 1 への同期
	ok1, ng1 := syncOneVercelTarget(&http.Client{}, "token1", "pid-1", "", items)
	// プロジェクト 2 への同期
	ok2, ng2 := syncOneVercelTarget(&http.Client{}, "token2", "pid-2", "", items)

	if ok1 != 1 || ng1 != 0 {
		t.Errorf("pid-1: ok=%d ng=%d, want ok=1 ng=0", ok1, ng1)
	}
	if ok2 != 1 || ng2 != 0 {
		t.Errorf("pid-2: ok=%d ng=%d, want ok=1 ng=0", ok2, ng2)
	}
	// 2 つの異なるプロジェクト ID への POST が飛んでいること
	if len(receivedPaths) != 2 {
		t.Errorf("receivedPaths len = %d, want 2", len(receivedPaths))
	}
}

func TestSyncOneVercelTarget_TeamIDInQuery(t *testing.T) {
	var receivedQuery string
	srv := newVercelTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			receivedQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusCreated)
		}
	})
	withVercelAPIBase(t, srv.URL)

	items := makeTestItems(t, []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: []string{"production"}},
	})
	syncOneVercelTarget(&http.Client{}, "token", "pid", "my-team-id", items)
	if !strings.Contains(receivedQuery, "teamId=my-team-id") {
		t.Errorf("query = %q, want teamId=my-team-id を含む", receivedQuery)
	}
}

func TestSyncOneVercelTarget_PartialFailure_ContinuesAndCounts(t *testing.T) {
	// 1 件目の送信が 500 でも 2 件目は送信され、失敗が集計されること。
	// 個別変数の送信失敗はループ内で継続する設計（os.Exit は呼ばない）の検証。
	var postCount int
	mu := &struct{ sync.Mutex }{}
	srv := newVercelTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		mu.Lock()
		postCount++
		n := postCount
		mu.Unlock()
		// 1 件目だけ失敗させる
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	withVercelAPIBase(t, srv.URL)

	items := makeTestItems(t, []provider.Entry{
		{Key: "FAIL_KEY", Value: "v1", Secret: true, Environments: []string{"production"}},
		{Key: "OK_KEY", Value: "v2", Secret: true, Environments: []string{"production"}},
	})
	ok, ng := syncOneVercelTarget(&http.Client{}, "token", "pid", "", items)
	if ok != 1 || ng != 1 {
		t.Errorf("ok=%d ng=%d, want ok=1 ng=1（1 件失敗しても残りを送信し集計する）", ok, ng)
	}
	mu.Lock()
	defer mu.Unlock()
	if postCount != 2 {
		t.Errorf("POST 回数 = %d, want 2（1 件目失敗後も 2 件目を送信する）", postCount)
	}
}

func TestSyncOneVercelTarget_FailingTargetDoesNotBlockNext(t *testing.T) {
	// 1 つ目のターゲットが全失敗（500）でも、2 つ目のターゲットへ POST が飛ぶこと。
	// Sync が複数ターゲットを順に処理する設計の最小検証（os.Exit を含む Sync 自体は範囲外）。
	var pid1Posts, pid2Posts int
	mu := &struct{ sync.Mutex }{}
	srv := newVercelTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		mu.Lock()
		switch {
		case strings.Contains(r.URL.Path, "/projects/pid-1/"):
			pid1Posts++
			mu.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
		case strings.Contains(r.URL.Path, "/projects/pid-2/"):
			pid2Posts++
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
		}
	})
	withVercelAPIBase(t, srv.URL)

	items := makeTestItems(t, []provider.Entry{
		{Key: "FOO", Value: "bar", Secret: true, Environments: []string{"production"}},
	})

	ok1, ng1 := syncOneVercelTarget(&http.Client{}, "token1", "pid-1", "", items)
	ok2, ng2 := syncOneVercelTarget(&http.Client{}, "token2", "pid-2", "", items)

	if ok1 != 0 || ng1 != 1 {
		t.Errorf("pid-1: ok=%d ng=%d, want ok=0 ng=1（全失敗）", ok1, ng1)
	}
	if ok2 != 1 || ng2 != 0 {
		t.Errorf("pid-2: ok=%d ng=%d, want ok=1 ng=0（前ターゲット失敗に影響されず成功）", ok2, ng2)
	}
	mu.Lock()
	defer mu.Unlock()
	if pid2Posts != 1 {
		t.Errorf("pid-2 への POST 回数 = %d, want 1（前ターゲットの失敗で中断されない）", pid2Posts)
	}
}

// newVercelTestServer は Vercel テスト用 HTTP サーバを立てる。
func newVercelTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// classifyVercelItems のテスト（分類結果の再利用確認のため）

func TestClassifyVercelItems_NewAndUpdate(t *testing.T) {
	items := []item{
		{Key: "NEW_KEY", Value: "v1", Type: "sensitive", Target: []string{"production"}},
		{Key: "EXISTING_KEY", Value: "v2", Type: "plain", Target: []string{"production"}},
	}
	existing := map[string]bool{
		"EXISTING_KEY": true,
	}
	classified := classifyVercelItems(items, existing)
	if len(classified) != 2 {
		t.Fatalf("classified len = %d, want 2", len(classified))
	}
	if !classified[0].isNew {
		t.Error("classified[0].isNew = false, want true (NEW_KEY は新規)")
	}
	if classified[1].isNew {
		t.Error("classified[1].isNew = true, want false (EXISTING_KEY は更新)")
	}
}

// --- filterEntriesByVercelProject のテスト ---

func TestFilterEntriesByVercelProject_NoFilter(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", VercelProjects: nil},
		{Key: "BAR", VercelProjects: []string{}},
	}
	got := filterEntriesByVercelProject(entries, "app-a")
	if len(got) != 2 {
		t.Errorf("len = %d, want 2 (VercelProjects 未指定は全ターゲット向け)", len(got))
	}
}

func TestFilterEntriesByVercelProject_MatchSingle(t *testing.T) {
	entries := []provider.Entry{
		{Key: "API_URL", VercelProjects: []string{"app-a"}},
		{Key: "DB_URL", VercelProjects: []string{"app-b"}},
		{Key: "SHARED", VercelProjects: nil},
	}
	got := filterEntriesByVercelProject(entries, "app-a")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (app-a + SHARED)", len(got))
	}
	keys := make([]string, len(got))
	for i, e := range got {
		keys[i] = e.Key
	}
	for _, want := range []string{"API_URL", "SHARED"} {
		found := false
		for _, k := range keys {
			if k == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("キー %q が結果に含まれていない: %v", want, keys)
		}
	}
}

func TestFilterEntriesByVercelProject_MatchMultiple(t *testing.T) {
	entries := []provider.Entry{
		{Key: "SHARED_KEY", VercelProjects: []string{"app-a", "app-b"}},
	}
	gotA := filterEntriesByVercelProject(entries, "app-a")
	gotB := filterEntriesByVercelProject(entries, "app-b")
	gotC := filterEntriesByVercelProject(entries, "app-c")
	if len(gotA) != 1 {
		t.Errorf("app-a: len = %d, want 1", len(gotA))
	}
	if len(gotB) != 1 {
		t.Errorf("app-b: len = %d, want 1", len(gotB))
	}
	if len(gotC) != 0 {
		t.Errorf("app-c: len = %d, want 0 (app-c は指定されていない)", len(gotC))
	}
}

// --- validateEntryVercelProjects のテスト ---

func TestValidateEntryVercelProjects_NoProjects_NoVercelProject(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", VercelProjects: nil},
	}
	if err := validateEntryVercelProjects(entries, nil); err != nil {
		t.Errorf("エラーなしを期待: %v", err)
	}
}

func TestValidateEntryVercelProjects_NoProjects_WithVercelProject(t *testing.T) {
	entries := []provider.Entry{
		{Key: "FOO", VercelProjects: []string{"app-a"}},
	}
	err := validateEntryVercelProjects(entries, nil)
	if err == nil {
		t.Error("エラーを期待（単一解決モードで vercel_project を指定）")
	}
	if !strings.Contains(err.Error(), "vercel.projects") {
		t.Errorf("エラーメッセージに 'vercel.projects' が含まれない: %v", err)
	}
}

func TestValidateEntryVercelProjects_ValidProject(t *testing.T) {
	projects := []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
		{Name: "app-b", ProjectID: "pid-b"},
	}
	entries := []provider.Entry{
		{Key: "API_URL", VercelProjects: []string{"app-a"}},
		{Key: "DB_URL", VercelProjects: []string{"app-a", "app-b"}},
	}
	if err := validateEntryVercelProjects(entries, projects); err != nil {
		t.Errorf("エラーなしを期待: %v", err)
	}
}

func TestValidateEntryVercelProjects_InvalidProject(t *testing.T) {
	projects := []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
	}
	entries := []provider.Entry{
		{Key: "FOO", VercelProjects: []string{"app-x"}},
	}
	err := validateEntryVercelProjects(entries, projects)
	if err == nil {
		t.Error("エラーを期待（存在しないプロジェクト名）")
	}
	if !strings.Contains(err.Error(), "app-x") {
		t.Errorf("エラーメッセージに 'app-x' が含まれない: %v", err)
	}
}

func TestValidateEntryVercelProjects_NoVercelProject_WithProjects(t *testing.T) {
	projects := []config.VercelProjectConf{
		{Name: "app-a", ProjectID: "pid-a"},
	}
	entries := []provider.Entry{
		{Key: "FOO", VercelProjects: nil},
	}
	if err := validateEntryVercelProjects(entries, projects); err != nil {
		t.Errorf("エラーなしを期待（vercel_project 未指定は全ターゲット向け）: %v", err)
	}
}
