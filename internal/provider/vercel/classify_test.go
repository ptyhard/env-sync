package vercel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- classifyVercelItems のユニットテスト（純粋関数） ---

func TestClassifyVercelItems_AllNew(t *testing.T) {
	items := []item{
		{Key: "FOO", Value: "v1", Type: "sensitive", Target: []string{"production"}},
		{Key: "BAR", Value: "v2", Type: "plain", Target: []string{"preview"}},
	}
	existing := map[string]bool{}

	result := classifyVercelItems(items, existing)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	for _, c := range result {
		if !c.isNew {
			t.Errorf("%s: isNew = false, want true", c.it.Key)
		}
	}
}

func TestClassifyVercelItems_AllUpdate(t *testing.T) {
	items := []item{
		{Key: "FOO", Value: "v1", Type: "sensitive", Target: []string{"production"}},
		{Key: "BAR", Value: "v2", Type: "plain", Target: []string{"preview"}},
	}
	existing := map[string]bool{"FOO": true, "BAR": true}

	result := classifyVercelItems(items, existing)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	for _, c := range result {
		if c.isNew {
			t.Errorf("%s: isNew = true, want false", c.it.Key)
		}
	}
}

func TestClassifyVercelItems_Mixed(t *testing.T) {
	items := []item{
		{Key: "NEW_KEY", Value: "v1", Type: "sensitive", Target: []string{"production"}},
		{Key: "EXISTING_KEY", Value: "v2", Type: "plain", Target: []string{"production"}},
	}
	existing := map[string]bool{"EXISTING_KEY": true}

	result := classifyVercelItems(items, existing)

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	if !result[0].isNew {
		t.Errorf("NEW_KEY: isNew = false, want true")
	}
	if result[1].isNew {
		t.Errorf("EXISTING_KEY: isNew = true, want false")
	}
}

func TestClassifyVercelItems_Empty(t *testing.T) {
	result := classifyVercelItems([]item{}, map[string]bool{})
	if len(result) != 0 {
		t.Errorf("len = %d, want 0", len(result))
	}
}

// --- countClassified のユニットテスト ---

func TestCountClassified_NilClassified(t *testing.T) {
	newCount, updateCount := countClassified(nil, 5)
	if newCount != 5 {
		t.Errorf("newCount = %d, want 5", newCount)
	}
	if updateCount != 0 {
		t.Errorf("updateCount = %d, want 0", updateCount)
	}
}

func TestCountClassified_Mixed(t *testing.T) {
	items := []item{
		{Key: "A"}, {Key: "B"}, {Key: "C"},
	}
	existing := map[string]bool{"B": true}
	classified := classifyVercelItems(items, existing)

	newCount, updateCount := countClassified(classified, len(items))
	if newCount != 2 {
		t.Errorf("newCount = %d, want 2", newCount)
	}
	if updateCount != 1 {
		t.Errorf("updateCount = %d, want 1", updateCount)
	}
}

// --- vercelFetchExistingKeys の統合テスト（httptest） ---

func TestVercelFetchExistingKeys_Success(t *testing.T) {
	var gotPath, gotQuery, gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		resp := map[string]interface{}{
			"envs": []map[string]interface{}{
				{"key": "FOO", "target": []string{"production"}},
				{"key": "BAR", "target": []string{"production", "preview"}},
				{"key": "FOO", "target": []string{"preview"}}, // 重複 key（実際の Vercel レスポンスで起こりうる）
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer srv.Close()

	// apiBase を httptest.Server に差し替えて実際に vercelFetchExistingKeys を呼ぶ。
	// これにより URL 組み立て・ヘッダー付与・ステータス処理・パースまで関数の契約を検証できる。
	origBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = origBase }()

	client := &http.Client{}
	existing, err := vercelFetchExistingKeys(client, "test-token", "test-project", "test-team")
	if err != nil {
		t.Fatalf("vercelFetchExistingKeys 失敗: %v", err)
	}

	// --- リクエストの組み立てを検証 ---
	if gotMethod != http.MethodGet {
		t.Errorf("メソッド = %q, want GET", gotMethod)
	}
	if want := "/v10/projects/test-project/env"; gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
	if want := "teamId=test-team"; gotQuery != want {
		t.Errorf("クエリ = %q, want %q", gotQuery, want)
	}
	if want := "Bearer test-token"; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}

	// --- レスポンスのパース結果を検証 ---
	if !existing["FOO"] {
		t.Error("FOO が存在しない")
	}
	if !existing["BAR"] {
		t.Error("BAR が存在しない")
	}
	if len(existing) != 2 {
		t.Errorf("existing 件数 = %d, want 2（重複 key は集約される）", len(existing))
	}
}

func TestVercelFetchExistingKeys_NoTeamID(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"envs": []map[string]interface{}{}}) //nolint:errcheck
	}))
	defer srv.Close()

	origBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = origBase }()

	if _, err := vercelFetchExistingKeys(&http.Client{}, "test-token", "test-project", ""); err != nil {
		t.Fatalf("vercelFetchExistingKeys 失敗: %v", err)
	}
	if gotQuery != "" {
		t.Errorf("teamID 空のときクエリは空のはず, got %q", gotQuery)
	}
}

func TestVercelFetchExistingKeys_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	origBase := apiBase
	apiBase = srv.URL
	defer func() { apiBase = origBase }()

	if _, err := vercelFetchExistingKeys(&http.Client{}, "bad-token", "test-project", ""); err == nil {
		t.Fatal("HTTP 401 のときエラーを返すべき")
	}
}
