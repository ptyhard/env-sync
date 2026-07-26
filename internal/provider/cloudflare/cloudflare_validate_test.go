package cloudflare

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ptyhard/env-sync/internal/i18n"
	"github.com/ptyhard/env-sync/internal/provider"
)

// captureOsExit はテスト中に validateOsExit を差し替えて終了コードをキャプチャする。
func captureOsExit(t *testing.T) *int {
	t.Helper()
	code := 0
	orig := validateOsExit
	validateOsExit = func(c int) { code = c }
	t.Cleanup(func() { validateOsExit = orig })
	return &code
}

// captureStdout はテスト中に stdoutWriter を差し替えて出力をキャプチャする。
func captureStdout(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := stdoutWriter
	stdoutWriter = &buf
	t.Cleanup(func() { stdoutWriter = orig })
	return &buf
}

// isolateConfig は config ファイルと wrangler 設定の影響を受けない一時ディレクトリへ移動する。
func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "")
	t.Chdir(t.TempDir())
	// 表示言語をテスト間で固定する（カタログ差異でアサーションが揺れないようにする）。
	// i18n のデフォルトは en のため、後始末も en に戻す。
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })
}

func TestValidate_ReportsOKOnReachableAPI(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	out := captureStdout(t)
	code := captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "HTTP 200") || !strings.Contains(got, "OK") {
		t.Errorf("到達成功が出力されていない:\n%s", got)
	}
	if !strings.Contains(got, "success 1 / failed 0") {
		t.Errorf("結果サマリが想定と異なる:\n%s", got)
	}
	if *code != 0 {
		t.Errorf("終了コード = %d, want 0", *code)
	}
}

func TestValidate_ShowsCauseOn404(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"success":false,"errors":[{"message":"script not found"}]}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "missing-worker")

	out := captureStdout(t)
	code := captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "HTTP 404") {
		t.Errorf("ステータスが出力されていない:\n%s", got)
	}
	if !strings.Contains(got, "Possible cause") {
		t.Errorf("推定原因が出力されていない:\n%s", got)
	}
	if *code != 1 {
		t.Errorf("終了コード = %d, want 1", *code)
	}
}

func TestValidate_SkipsAPICheckWhenTokenUnset(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, nil)
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	out := captureStdout(t)
	code := captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "token is not set") {
		t.Errorf("トークン未設定のスキップメッセージが無い:\n%s", got)
	}
	if *code != 1 {
		t.Errorf("終了コード = %d, want 1", *code)
	}
}

func TestValidate_SkipsAPICheckWhenAccountIDUnset(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, nil)
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	out := captureStdout(t)
	captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "accountId is not set") {
		t.Errorf("accountId 未設定のスキップメッセージが無い:\n%s", got)
	}
}

func TestValidate_SkipsAPICheckWhenScriptUnresolvable(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, nil)
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")

	out := captureStdout(t)
	captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	if got := out.String(); !strings.Contains(got, "script name could not be resolved") {
		t.Errorf("script 未解決のスキップメッセージが無い:\n%s", got)
	}
}

func TestValidate_DoesNotWrite(t *testing.T) {
	isolateConfig(t)
	_, requests := newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	captureStdout(t)
	captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	for _, req := range *requests {
		if req.method != http.MethodGet {
			t.Errorf("validate が %s リクエストを送信した（GET のみのはず）: %s", req.method, req.path)
		}
	}
}

func TestValidate_ShowsSourceLabels(t *testing.T) {
	isolateConfig(t)
	newRecordingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"success":true,"result":[]}`) //nolint:errcheck
	})
	t.Setenv("CLOUDFLARE_API_TOKEN", "tok")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct1")
	t.Setenv("CLOUDFLARE_SCRIPT_NAME", "my-worker")

	out := captureStdout(t)
	captureOsExit(t)

	p := &cloudflareProvider{}
	if err := p.Validate(provider.Options{}, nil); err != nil {
		t.Fatalf("エラー: %v", err)
	}
	got := out.String()
	// トークンの値そのものは出力しない
	if strings.Contains(got, "tok") && !strings.Contains(got, "[set]") {
		t.Errorf("トークン値が出力されている可能性:\n%s", got)
	}
	if !strings.Contains(got, "env var") {
		t.Errorf("取得元ラベルが出力されていない:\n%s", got)
	}
}

// --- sourceLabel のテスト ---

func TestSourceLabel(t *testing.T) {
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })
	tests := []struct{ src, want string }{
		{"env", "env var"},
		{"config", "config file"},
		{"wrangler", "wrangler config"},
		{"", "(unset)"},
		{"unknown", "(unset)"},
	}
	for _, tt := range tests {
		if got := sourceLabel(tt.src); got != tt.want {
			t.Errorf("sourceLabel(%q) = %q, want %q", tt.src, got, tt.want)
		}
	}
}
