package vercel

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

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
