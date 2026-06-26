package main

import (
	"testing"
)

// resolveEntries のユニットテスト

// secret は明示指定が nil のとき defaults.secret を継承し、それも nil なら true
func TestResolveEntries_SecretDefault_True(t *testing.T) {
	def := definition{
		Variables: map[string]varConf{
			"FOO": {},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if !entries[0].Secret {
		t.Error("Secret = false, want true (デフォルトは安全側の true)")
	}
}

// defaults.secret = false のとき Secret=false を継承
func TestResolveEntries_SecretInheritFromDefaults_False(t *testing.T) {
	f := false
	def := definition{}
	def.Defaults.Secret = &f
	def.Variables = map[string]varConf{
		"FOO": {},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if entries[0].Secret {
		t.Error("Secret = true, want false (defaults.secret=false を継承)")
	}
}

// varConf.Secret = false で明示上書き
func TestResolveEntries_SecretExplicitFalse(t *testing.T) {
	f := false
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Secret: &f},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if entries[0].Secret {
		t.Error("Secret = true, want false (varConf.Secret=false の明示)")
	}
}

// varConf.Secret = true で明示 (defaults が false でも上書き)
func TestResolveEntries_SecretExplicitTrue_OverridesDefault(t *testing.T) {
	f := false
	tr := true
	def := definition{}
	def.Defaults.Secret = &f
	def.Variables = map[string]varConf{
		"FOO": {Secret: &tr},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if !entries[0].Secret {
		t.Error("Secret = false, want true (varConf.Secret=true で明示上書き)")
	}
}

// environments は varConf に指定があれば採用
func TestResolveEntries_EnvironmentsExplicit(t *testing.T) {
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Environments: []string{"production"}},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Environments) != 1 || entries[0].Environments[0] != "production" {
		t.Errorf("Environments = %v, want [production]", entries[0].Environments)
	}
}

// environments は defaults から継承される
func TestResolveEntries_EnvironmentsInheritFromDefaults(t *testing.T) {
	def := definition{}
	def.Defaults.Environments = []string{"production", "preview"}
	def.Variables = map[string]varConf{
		"FOO": {},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Environments) != 2 {
		t.Errorf("Environments = %v, want [production preview]", entries[0].Environments)
	}
}

// environments も defaults も未指定なら空のまま（provider 側でフォールバック）
func TestResolveEntries_EnvironmentsEmpty(t *testing.T) {
	def := definition{
		Variables: map[string]varConf{
			"FOO": {},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Environments) != 0 {
		t.Errorf("Environments = %v, want []", entries[0].Environments)
	}
}

// def にあるが env に無いキーはスキップ
func TestResolveEntries_SkipsKeyNotInEnv(t *testing.T) {
	def := definition{
		Variables: map[string]varConf{
			"FOO": {},
			"BAR": {},
		},
	}
	envVars := map[string]string{"FOO": "bar"} // BAR は env に無い
	entries, err := resolveEntries(def, envVars, []string{"BAR", "FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1 (BAR はスキップ)", len(entries))
	}
	if entries[0].Key != "FOO" {
		t.Errorf("entries[0].Key = %q, want FOO", entries[0].Key)
	}
}

// varConf の environments 指定が defaults の environments より優先される
func TestResolveEntries_EnvironmentsVarConfOverridesDefaults(t *testing.T) {
	def := definition{}
	def.Defaults.Environments = []string{"production", "preview"}
	def.Variables = map[string]varConf{
		"FOO": {Environments: []string{"development"}},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Environments) != 1 || entries[0].Environments[0] != "development" {
		t.Errorf("Environments = %v, want [development]", entries[0].Environments)
	}
}

// Key/Value が正しくセットされる
func TestResolveEntries_KeyValue(t *testing.T) {
	def := definition{
		Variables: map[string]varConf{
			"MY_KEY": {},
		},
	}
	envVars := map[string]string{"MY_KEY": "my-value"}
	entries, err := resolveEntries(def, envVars, []string{"MY_KEY"})
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if entries[0].Key != "MY_KEY" {
		t.Errorf("Key = %q, want MY_KEY", entries[0].Key)
	}
	if entries[0].Value != "my-value" {
		t.Errorf("Value = %q, want my-value", entries[0].Value)
	}
}
