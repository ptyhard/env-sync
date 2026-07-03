package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ptyhard/env-sync/internal/provider"
)

// keepFunc はテスト用に Options.PruneKeep 相当の保持判定を作るヘルパー。
func keepFunc(definedKeys ...string) func(string) bool {
	opts := provider.Options{DefinedKeys: definedKeys}
	return opts.PruneKeep()
}

// --- pruneScopes のテスト ---

func TestPruneScopes_AlwaysIncludesRepoLevel(t *testing.T) {
	scopes := pruneScopes(nil, nil)
	if len(scopes) != 1 || scopes[0] != "" {
		t.Errorf("scopes = %v, want [\"\"]（repo レベルは常にスキャン）", scopes)
	}
}

func TestPruneScopes_CollectsUniqueEnvScopes(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "A"}},
		{envScope: "production", entry: provider.Entry{Key: "B"}},
		{envScope: "production", entry: provider.Entry{Key: "C"}},
		{envScope: "staging", entry: provider.Entry{Key: "D"}},
	}
	scopes := pruneScopes(tasks, nil)
	want := []string{"", "production", "staging"}
	if len(scopes) != len(want) {
		t.Fatalf("scopes = %v, want %v", scopes, want)
	}
	for i, s := range want {
		if scopes[i] != s {
			t.Errorf("scopes[%d] = %q, want %q", i, scopes[i], s)
		}
	}
}

// --- pruneScopes + filterEnvs のテスト ---

// filterEnvs 指定時は repo レベル("") を除外すること
func TestPruneScopes_FilterEnvs_ExcludesRepoLevel(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "A"}},
		{envScope: "production", entry: provider.Entry{Key: "B"}},
	}
	scopes := pruneScopes(tasks, []string{"production"})
	for _, s := range scopes {
		if s == "" {
			t.Errorf("scopes に repo レベル (\"\") が含まれている: %v（filterEnvs 指定時は除外するべき）", scopes)
		}
	}
	if len(scopes) != 1 || scopes[0] != "production" {
		t.Errorf("scopes = %v, want [production]（指定環境のみ）", scopes)
	}
}

// filterEnvs に含まれない named environment はスコープに入らないこと
func TestPruneScopes_FilterEnvs_OnlyIncludesSpecifiedEnvs(t *testing.T) {
	tasks := []githubTask{
		{envScope: "production", entry: provider.Entry{Key: "A"}},
		{envScope: "staging", entry: provider.Entry{Key: "B"}},
	}
	scopes := pruneScopes(tasks, []string{"production"})
	if len(scopes) != 1 || scopes[0] != "production" {
		t.Errorf("scopes = %v, want [production]（staging は filterEnvs 外なので除外）", scopes)
	}
}

// filterEnvs が空のとき従来の動作（repo レベルを含む全スコープ）を返すこと（後方互換）
func TestPruneScopes_FilterEnvs_Empty_BackwardCompat(t *testing.T) {
	tasks := []githubTask{
		{envScope: "", entry: provider.Entry{Key: "A"}},
		{envScope: "production", entry: provider.Entry{Key: "B"}},
	}
	scopes := pruneScopes(tasks, nil)
	if len(scopes) != 2 || scopes[0] != "" || scopes[1] != "production" {
		t.Errorf("scopes = %v, want [\"\", \"production\"]（後方互換）", scopes)
	}
}

// --- undefinedNames のテスト ---

func TestUndefinedNames(t *testing.T) {
	names := []string{"DEFINED_KEY", "STALE_KEY", "OTHER"}
	got := undefinedNames(names, keepFunc("DEFINED_KEY"))
	if len(got) != 2 || got[0] != "STALE_KEY" || got[1] != "OTHER" {
		t.Errorf("undefinedNames = %v, want [STALE_KEY OTHER]", got)
	}
}

func TestUndefinedNames_CaseInsensitive(t *testing.T) {
	// GitHub は名前を大文字で返すため、小文字で定義されたキーも保持される
	got := undefinedNames([]string{"API_KEY"}, keepFunc("api_key"))
	if len(got) != 0 {
		t.Errorf("undefinedNames = %v, want 空（大文字小文字非区別）", got)
	}
}

// --- githubListNames のテスト ---

func TestGithubListNames_SecretsRepoLevel(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 2,
			"secrets":     []map[string]string{{"name": "FOO"}, {"name": "BAR"}},
		})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	names, err := githubListNames(&http.Client{}, "token", "owner", "repo", "", true)
	if err != nil {
		t.Fatalf("githubListNames: %v", err)
	}
	if want := "/repos/owner/repo/actions/secrets"; gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
	if len(names) != 2 || names[0] != "FOO" || names[1] != "BAR" {
		t.Errorf("names = %v, want [FOO BAR]", names)
	}
}

func TestGithubListNames_VariablesEnvScope(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"total_count": 1,
			"variables":   []map[string]string{{"name": "MY_VAR"}},
		})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	names, err := githubListNames(&http.Client{}, "token", "owner", "repo", "production", false)
	if err != nil {
		t.Fatalf("githubListNames: %v", err)
	}
	if want := "/repos/owner/repo/environments/production/variables"; gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
	if len(names) != 1 || names[0] != "MY_VAR" {
		t.Errorf("names = %v, want [MY_VAR]", names)
	}
}

func TestGithubListNames_Pagination(t *testing.T) {
	// 1 ページ目に 100 件、2 ページ目に 1 件を返し、全 101 件が集まること
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var secrets []map[string]string
		if page == 1 {
			for i := 0; i < 100; i++ {
				secrets = append(secrets, map[string]string{"name": fmt.Sprintf("KEY_%d", i)})
			}
		} else {
			secrets = []map[string]string{{"name": "LAST_KEY"}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"total_count": 101, "secrets": secrets})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	names, err := githubListNames(&http.Client{}, "token", "owner", "repo", "", true)
	if err != nil {
		t.Fatalf("githubListNames: %v", err)
	}
	if len(names) != 101 {
		t.Errorf("names 件数 = %d, want 101（ページングで全件取得）", len(names))
	}
	if names[100] != "LAST_KEY" {
		t.Errorf("names[100] = %q, want LAST_KEY", names[100])
	}
}

func TestGithubListNames_ExactMultipleOfPerPage_NoExtraRequest(t *testing.T) {
	// 件数がちょうど 100（per_page の倍数）のとき、total_count により追加の空ページ取得をしないこと
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var secrets []map[string]string
		for i := 0; i < 100; i++ {
			secrets = append(secrets, map[string]string{"name": fmt.Sprintf("KEY_%d", i)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"total_count": 100, "secrets": secrets})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	names, err := githubListNames(&http.Client{}, "token", "owner", "repo", "", true)
	if err != nil {
		t.Fatalf("githubListNames: %v", err)
	}
	if len(names) != 100 {
		t.Errorf("names 件数 = %d, want 100", len(names))
	}
	if requests != 1 {
		t.Errorf("リクエスト回数 = %d, want 1（total_count 到達で追加ページを取得しない）", requests)
	}
}

func TestGithubListNames_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"forbidden"}`))
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	if _, err := githubListNames(&http.Client{}, "token", "owner", "repo", "", true); err == nil {
		t.Fatal("HTTP 403 のときエラーを返すべき")
	}
}

// --- githubDeletePruneTarget のテスト ---

func TestGithubDeletePruneTarget_SecretRepoLevel(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	err := githubDeletePruneTarget(&http.Client{}, "token", "owner", "repo",
		githubPruneTarget{envScope: "", name: "STALE_SECRET", secret: true})
	if err != nil {
		t.Fatalf("githubDeletePruneTarget: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("メソッド = %q, want DELETE", gotMethod)
	}
	if want := "/repos/owner/repo/actions/secrets/STALE_SECRET"; gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
}

func TestGithubDeletePruneTarget_VariableEnvScope(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	err := githubDeletePruneTarget(&http.Client{}, "token", "owner", "repo",
		githubPruneTarget{envScope: "production", name: "STALE_VAR", secret: false})
	if err != nil {
		t.Fatalf("githubDeletePruneTarget: %v", err)
	}
	if want := "/repos/owner/repo/environments/production/variables/STALE_VAR"; gotPath != want {
		t.Errorf("パス = %q, want %q", gotPath, want)
	}
}

func TestGithubDeletePruneTarget_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	err := githubDeletePruneTarget(&http.Client{}, "token", "owner", "repo",
		githubPruneTarget{name: "GONE", secret: true})
	if err == nil {
		t.Fatal("HTTP 404 のときエラーを返すべき")
	}
}

// --- collectGitHubPrune のテスト ---

func TestCollectGitHubPrune(t *testing.T) {
	// repo レベル: secrets に STALE_SECRET / DEFINED_KEY、variables に STALE_VAR
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/owner/repo/actions/secrets":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"secrets": []map[string]string{{"name": "STALE_SECRET"}, {"name": "DEFINED_KEY"}},
			})
		case "/repos/owner/repo/actions/variables":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"variables": []map[string]string{{"name": "STALE_VAR"}},
			})
		default:
			t.Errorf("予期しないパス: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	got, err := collectGitHubPrune(&http.Client{}, "token", "owner", "repo", []string{""}, keepFunc("DEFINED_KEY"))
	if err != nil {
		t.Fatalf("collectGitHubPrune: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("prune 対象件数 = %d, want 2: %v", len(got), got)
	}
	if got[0].name != "STALE_SECRET" || !got[0].secret {
		t.Errorf("got[0] = %+v, want STALE_SECRET (Secret)", got[0])
	}
	if got[1].name != "STALE_VAR" || got[1].secret {
		t.Errorf("got[1] = %+v, want STALE_VAR (Variable)", got[1])
	}
}
