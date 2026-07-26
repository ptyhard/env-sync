package cloudflare

import (
	"strings"
	"testing"

	"github.com/ptyhard/env-sync/internal/provider"
)

// --- resolveScriptName のテスト ---

func TestResolveScriptName(t *testing.T) {
	mapping := map[string]string{"production": "my-worker", "staging": "stg-worker"}
	tests := []struct {
		name    string
		base    string
		env     string
		mapping map[string]string
		want    string
	}{
		{"環境が空ならベーススクリプト", "my-worker", "", nil, "my-worker"},
		{"マッピング無しならサフィックス", "my-worker", "staging", nil, "my-worker-staging"},
		{"マッピングがあれば優先", "my-worker", "staging", mapping, "stg-worker"},
		{"マッピングで production をベースに寄せられる", "my-worker", "production", mapping, "my-worker"},
		{"マッピングに無い環境はサフィックス", "my-worker", "preview", mapping, "my-worker-preview"},
		{"マッピング値が空ならサフィックスにフォールバック", "my-worker", "dev", map[string]string{"dev": ""}, "my-worker-dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveScriptName(tt.base, tt.env, tt.mapping); got != tt.want {
				t.Errorf("resolveScriptName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- expandCloudflareTasks のテスト ---

func TestExpandCloudflareTasks_NoEnvironmentsUsesBaseScript(t *testing.T) {
	entries := []provider.Entry{{Key: "API_KEY", Value: "v", Secret: true}}
	got := expandCloudflareTasks(entries, "my-worker", nil)
	if len(got) != 1 {
		t.Fatalf("task 数 = %d, want 1", len(got))
	}
	if got[0].script != "my-worker" || got[0].entry.Key != "API_KEY" {
		t.Errorf("task = %+v, want script=my-worker key=API_KEY", got[0])
	}
}

func TestExpandCloudflareTasks_ExpandsPerEnvironment(t *testing.T) {
	entries := []provider.Entry{
		{Key: "DB_URL", Value: "v", Secret: true, Environments: []string{"production", "staging"}},
	}
	got := expandCloudflareTasks(entries, "my-worker", nil)
	if len(got) != 2 {
		t.Fatalf("task 数 = %d, want 2", len(got))
	}
	want := []string{"my-worker-production", "my-worker-staging"}
	for i, w := range want {
		if got[i].script != w {
			t.Errorf("task[%d].script = %q, want %q", i, got[i].script, w)
		}
	}
}

func TestExpandCloudflareTasks_DeduplicatesSameResolvedScript(t *testing.T) {
	// production と prod がどちらも同じスクリプトへ解決される場合、二重送信しない
	mapping := map[string]string{"production": "my-worker", "prod": "my-worker"}
	entries := []provider.Entry{
		{Key: "DB_URL", Value: "v", Secret: true, Environments: []string{"production", "prod"}},
	}
	got := expandCloudflareTasks(entries, "my-worker", mapping)
	if len(got) != 1 {
		t.Fatalf("task 数 = %d, want 1（同一スクリプトは重複排除）", len(got))
	}
}

func TestExpandCloudflareTasks_EmptyEntries(t *testing.T) {
	if got := expandCloudflareTasks(nil, "my-worker", nil); len(got) != 0 {
		t.Errorf("task 数 = %d, want 0", len(got))
	}
}

// --- taskScripts のテスト ---

func TestTaskScripts_DeduplicatesPreservingOrder(t *testing.T) {
	tasks := []cloudflareTask{
		{script: "b"}, {script: "a"}, {script: "b"}, {script: "c"},
	}
	got := taskScripts(tasks)
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("スクリプト数 = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scripts[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// --- classifyTasks / countClassified のテスト ---

func TestClassifyTasks_NewAndUpdate(t *testing.T) {
	tasks := []cloudflareTask{
		{script: "w", entry: provider.Entry{Key: "EXISTING"}},
		{script: "w", entry: provider.Entry{Key: "FRESH"}},
		{script: "w-staging", entry: provider.Entry{Key: "EXISTING"}},
	}
	existing := map[string]map[string]bool{
		"w": {"EXISTING": true},
	}
	got := classifyTasks(tasks, existing)
	if got[0].isNew {
		t.Error("EXISTING (script=w) は更新扱いのはず")
	}
	if !got[1].isNew {
		t.Error("FRESH は新規扱いのはず")
	}
	if !got[2].isNew {
		t.Error("EXISTING (script=w-staging) は別スクリプトのため新規扱いのはず")
	}
}

func TestCountClassified(t *testing.T) {
	classified := []classifiedTask{{isNew: true}, {isNew: false}, {isNew: true}}
	newCount, updateCount := countClassified(classified, 3)
	if newCount != 2 || updateCount != 1 {
		t.Errorf("(new, update) = (%d, %d), want (2, 1)", newCount, updateCount)
	}
	// nil のときは全件新規として扱う
	newCount, updateCount = countClassified(nil, 5)
	if newCount != 5 || updateCount != 0 {
		t.Errorf("nil 時 (new, update) = (%d, %d), want (5, 0)", newCount, updateCount)
	}
}

// --- computePrune のテスト ---

// keepFunc はテスト用に Options.PruneKeep 相当の保持判定を作るヘルパー。
func keepFunc(definedKeys ...string) func(string) bool {
	opts := provider.Options{DefinedKeys: definedKeys}
	return opts.PruneKeep()
}

func TestComputePrune_UndefinedKeyIsPruned(t *testing.T) {
	existing := map[string][]string{"w": {"DEFINED_KEY", "STALE_KEY"}}
	got := computePrune([]string{"w"}, existing, keepFunc("DEFINED_KEY"))
	if len(got) != 1 || got[0].name != "STALE_KEY" || got[0].script != "w" {
		t.Errorf("prune 対象 = %+v, want [{w STALE_KEY}]", got)
	}
}

func TestComputePrune_AllDefinedIsEmpty(t *testing.T) {
	existing := map[string][]string{"w": {"A", "B"}}
	if got := computePrune([]string{"w"}, existing, keepFunc("A", "B")); len(got) != 0 {
		t.Errorf("prune 対象 = %+v, want []", got)
	}
}

func TestComputePrune_ScansEachScript(t *testing.T) {
	existing := map[string][]string{
		"w":         {"STALE_A"},
		"w-staging": {"STALE_B"},
	}
	got := computePrune([]string{"w", "w-staging"}, existing, keepFunc())
	if len(got) != 2 {
		t.Fatalf("prune 対象数 = %d, want 2", len(got))
	}
	// scripts の順に走査されるため決定的な順序になる
	if got[0].script != "w" || got[1].script != "w-staging" {
		t.Errorf("走査順 = %+v, want [w, w-staging]", got)
	}
}

func TestComputePrune_ExcludePatternIsKept(t *testing.T) {
	opts := provider.Options{DefinedKeys: []string{"KEEP"}, PruneExclude: []string{"BLOB_*"}}
	existing := map[string][]string{"w": {"KEEP", "BLOB_TOKEN", "STALE"}}
	got := computePrune([]string{"w"}, existing, opts.PruneKeep())
	if len(got) != 1 || got[0].name != "STALE" {
		t.Errorf("prune 対象 = %+v, want [STALE]", got)
	}
}

// --- parseErrorBody のテスト ---

func TestParseErrorBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"message を取り出す", `{"success":false,"errors":[{"code":10007,"message":"script not found"}]}`, "script not found"},
		{"複数エラーを連結", `{"errors":[{"message":"a"},{"message":"b"}]}`, "a; b"},
		{"message が空なら code", `{"errors":[{"code":1234}]}`, "code 1234"},
		{"errors が空なら空文字", `{"success":true,"errors":[]}`, ""},
		{"JSON でなければ空文字", `not json`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseErrorBody([]byte(tt.body)); got != tt.want {
				t.Errorf("parseErrorBody() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- secretsURL のテスト ---

func TestSecretsURL_EscapesPathSegments(t *testing.T) {
	got := secretsURL("acct/1", "my worker")
	if strings.Contains(got, "acct/1") {
		t.Errorf("accountID がエスケープされていない: %s", got)
	}
	if !strings.HasSuffix(got, "/secrets") {
		t.Errorf("URL の末尾が /secrets でない: %s", got)
	}
	if !strings.Contains(got, "my%20worker") {
		t.Errorf("script 名がエスケープされていない: %s", got)
	}
}

// --- tomlTopLevelName のテスト ---

func TestTomlTopLevelName(t *testing.T) {
	tests := []struct {
		name string
		toml string
		want string
	}{
		{
			name: "トップレベルの name を取得",
			toml: "name = \"my-worker\"\nmain = \"src/index.ts\"\n",
			want: "my-worker",
		},
		{
			name: "テーブル内の name は無視する",
			toml: "main = \"src/index.ts\"\n\n[vars]\nname = \"not-this\"\n",
			want: "",
		},
		{
			name: "テーブルより前の name のみ採用",
			toml: "name = \"base\"\n\n[env.staging]\nname = \"staging-worker\"\n",
			want: "base",
		},
		{
			name: "行末コメントを除去",
			toml: "name = \"my-worker\"  # 本番の Worker\n",
			want: "my-worker",
		},
		{
			name: "コメント行と空行を読み飛ばす",
			toml: "# コメント\n\nname = 'single-quoted'\n",
			want: "single-quoted",
		},
		{
			name: "name が無ければ空",
			toml: "main = \"src/index.ts\"\n",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tomlTopLevelName(tt.toml); got != tt.want {
				t.Errorf("tomlTopLevelName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- stripJSONComments のテスト ---

func TestStripJSONComments(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"行コメントを除去", "{\n // comment\n \"a\": 1\n}", "{\n \n \"a\": 1\n}"},
		{"ブロックコメントを除去", "{/* c */\"a\": 1}", "{\"a\": 1}"},
		{"文字列内のスラッシュは保持", `{"url": "https://example.com"}`, `{"url": "https://example.com"}`},
		{"文字列内の // は保持", `{"a": "x // y"}`, `{"a": "x // y"}`},
		{"エスケープされた引用符を跨がない", `{"a": "he said \"hi\"", "b": 1}`, `{"a": "he said \"hi\"", "b": 1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripJSONComments(tt.in); got != tt.want {
				t.Errorf("stripJSONComments() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- parseWranglerName のテスト ---

func TestParseWranglerName(t *testing.T) {
	tests := []struct {
		name string
		path string
		text string
		want string
	}{
		{"toml", "wrangler.toml", "name = \"toml-worker\"\n", "toml-worker"},
		{"json", "wrangler.json", `{"name": "json-worker"}`, "json-worker"},
		{"jsonc（コメント付き）", "wrangler.jsonc", "{\n // Worker 名\n \"name\": \"jsonc-worker\"\n}", "jsonc-worker"},
		{"不正な JSON は空", "wrangler.json", `{invalid`, ""},
		{"name フィールドが無ければ空", "wrangler.json", `{"main": "src/index.ts"}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseWranglerName(tt.path, tt.text); got != tt.want {
				t.Errorf("parseWranglerName() = %q, want %q", got, tt.want)
			}
		})
	}
}
