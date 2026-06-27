package github

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

// withGitHubAPIBase はテスト中だけ githubAPIBase をテストサーバに差し替える。
func withGitHubAPIBase(t *testing.T, base string) {
	t.Helper()
	orig := githubAPIBase
	githubAPIBase = base
	t.Cleanup(func() { githubAPIBase = orig })
}

// genTestPublicKey は base64 エンコードした 32 バイトの curve25519 公開鍵を返す。
func genTestPublicKey(t *testing.T) (b64 string, pub *[32]byte) {
	t.Helper()
	pubKey, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("鍵生成失敗: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pubKey[:]), pubKey
}

func TestGitHubPublicKey_Success(t *testing.T) {
	b64Key, _ := genTestPublicKey(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/actions/secrets/public-key" {
			t.Errorf("path = %s, want repo-level public-key", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok123" {
			t.Errorf("Authorization = %q, want Bearer tok123", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Errorf("X-GitHub-Api-Version = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"key_id": "kid-1", "key": b64Key})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	keyID, key, err := githubPublicKey(srv.Client(), "tok123", "owner", "repo", "")
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if keyID != "kid-1" {
		t.Errorf("keyID = %q, want kid-1", keyID)
	}
	if key == nil || len(base64.StdEncoding.EncodeToString(key[:])) == 0 {
		t.Error("公開鍵が取得できていない")
	}
	if base64.StdEncoding.EncodeToString(key[:]) != b64Key {
		t.Error("公開鍵がレスポンスと一致しない")
	}
}

func TestGitHubPublicKey_EnvironmentScope(t *testing.T) {
	b64Key, _ := genTestPublicKey(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/environments/staging/secrets/public-key" {
			t.Errorf("path = %s, want environment-scoped public-key", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"key_id": "kid-2", "key": b64Key})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	_, _, err := githubPublicKey(srv.Client(), "tok", "owner", "repo", "staging")
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

func TestGitHubPublicKey_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	_, _, err := githubPublicKey(srv.Client(), "tok", "owner", "repo", "")
	if err == nil {
		t.Fatal("エラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "Not Found") {
		t.Errorf("エラーメッセージに API メッセージが含まれていない: %v", err)
	}
}

func TestGitHubPublicKey_InvalidKeyLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 32 バイトでない不正な鍵を返す
		_ = json.NewEncoder(w).Encode(map[string]string{
			"key_id": "kid",
			"key":    base64.StdEncoding.EncodeToString([]byte("too-short")),
		})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	_, _, err := githubPublicKey(srv.Client(), "tok", "owner", "repo", "")
	if err == nil {
		t.Fatal("不正な鍵長でエラーを期待したが nil")
	}
}

func TestGitHubPutSecret_Success(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusNoContent} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				t.Errorf("method = %s, want PUT", r.Method)
			}
			if r.URL.Path != "/repos/owner/repo/actions/secrets/MY_SECRET" {
				t.Errorf("path = %s", r.URL.Path)
			}
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["encrypted_value"] != "enc==" {
				t.Errorf("encrypted_value = %q", body["encrypted_value"])
			}
			if body["key_id"] != "kid" {
				t.Errorf("key_id = %q", body["key_id"])
			}
			w.WriteHeader(status)
		}))
		withGitHubAPIBase(t, srv.URL)
		err := githubPutSecret(srv.Client(), "tok", "owner", "repo", "", "MY_SECRET", "enc==", "kid")
		if err != nil {
			t.Errorf("status %d で予期しないエラー: %v", status, err)
		}
		srv.Close()
	}
}

func TestGitHubPutSecret_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Invalid request"})
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	err := githubPutSecret(srv.Client(), "tok", "owner", "repo", "", "S", "enc", "kid")
	if err == nil {
		t.Fatal("エラーを期待したが nil")
	}
	if !strings.Contains(err.Error(), "Invalid request") {
		t.Errorf("API メッセージが含まれていない: %v", err)
	}
}

func TestGitHubVariableExists(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   bool
		errOK  bool
	}{
		{"存在する", http.StatusOK, true, false},
		{"存在しない", http.StatusNotFound, false, false},
		{"サーバエラー", http.StatusInternalServerError, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repos/owner/repo/actions/variables/MY_VAR" {
					t.Errorf("path = %s", r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			withGitHubAPIBase(t, srv.URL)

			got, err := githubVariableExists(srv.Client(), "tok", "owner", "repo", "", "MY_VAR")
			if tc.errOK && err == nil {
				t.Fatal("エラーを期待したが nil")
			}
			if !tc.errOK && err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got != tc.want {
				t.Errorf("exists = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitHubCreateVariable_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/actions/variables" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "MY_VAR" || body["value"] != "hello" {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	if err := githubCreateVariable(srv.Client(), "tok", "owner", "repo", "", "MY_VAR", "hello"); err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

func TestGitHubUpdateVariable_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/actions/variables/MY_VAR" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	if err := githubUpdateVariable(srv.Client(), "tok", "owner", "repo", "", "MY_VAR", "hello"); err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

func TestGitHubCreateVariable_EnvironmentScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/environments/prod/variables" {
			t.Errorf("path = %s, want environment-scoped variables", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	if err := githubCreateVariable(srv.Client(), "tok", "owner", "repo", "prod", "V", "x"); err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

func TestGitHubPutSecret_EnvironmentScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/environments/staging/secrets/MY_SECRET" {
			t.Errorf("path = %s, want environment-scoped secret PUT", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["encrypted_value"] != "enc==" {
			t.Errorf("encrypted_value = %q", body["encrypted_value"])
		}
		if body["key_id"] != "kid" {
			t.Errorf("key_id = %q", body["key_id"])
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	if err := githubPutSecret(srv.Client(), "tok", "owner", "repo", "staging", "MY_SECRET", "enc==", "kid"); err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

func TestGitHubVariableExists_EnvironmentScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/environments/staging/variables/MY_VAR" {
			t.Errorf("path = %s, want environment-scoped variable GET", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	got, err := githubVariableExists(srv.Client(), "tok", "owner", "repo", "staging", "MY_VAR")
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if !got {
		t.Error("exists = false, want true")
	}
}

func TestGitHubUpdateVariable_EnvironmentScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/environments/staging/variables/MY_VAR" {
			t.Errorf("path = %s, want environment-scoped variable PATCH", r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "MY_VAR" || body["value"] != "hello" {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	if err := githubUpdateVariable(srv.Client(), "tok", "owner", "repo", "staging", "MY_VAR", "hello"); err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
}

func TestGitHubSecretExists(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   bool
		errOK  bool
	}{
		{"存在する", http.StatusOK, true, false},
		{"存在しない", http.StatusNotFound, false, false},
		{"サーバエラー", http.StatusInternalServerError, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("method = %s, want GET", r.Method)
				}
				if r.URL.Path != "/repos/owner/repo/actions/secrets/MY_SECRET" {
					t.Errorf("path = %s, want repo-level secret GET", r.URL.Path)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			withGitHubAPIBase(t, srv.URL)

			got, err := githubSecretExists(srv.Client(), "tok", "owner", "repo", "", "MY_SECRET")
			if tc.errOK && err == nil {
				t.Fatal("エラーを期待したが nil")
			}
			if !tc.errOK && err != nil {
				t.Fatalf("予期しないエラー: %v", err)
			}
			if got != tc.want {
				t.Errorf("exists = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitHubSecretExists_EnvironmentScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/environments/staging/secrets/MY_SECRET" {
			t.Errorf("path = %s, want environment-scoped secret GET", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	withGitHubAPIBase(t, srv.URL)

	got, err := githubSecretExists(srv.Client(), "tok", "owner", "repo", "staging", "MY_SECRET")
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if !got {
		t.Error("exists = false, want true")
	}
}

func TestParseGitHubErrorBody(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"メッセージあり", `{"message":"Bad credentials"}`, "Bad credentials"},
		{"メッセージなし", `{"foo":"bar"}`, ""},
		{"不正な JSON", `not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseGitHubErrorBody(io.NopCloser(strings.NewReader(tc.body)))
			if got != tc.want {
				t.Errorf("parseGitHubErrorBody = %q, want %q", got, tc.want)
			}
		})
	}
}
