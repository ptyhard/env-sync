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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"BAR", "FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
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
	entries, err := resolveEntries(def, envVars, []string{"MY_KEY"}, "vercel")
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

// provider が varConf で指定されるとそれが使われる
func TestResolveEntries_ProviderExplicitVarConf(t *testing.T) {
	pv := &ProviderVal{Values: []string{"github"}}
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Provider: pv},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Providers) != 1 || entries[0].Providers[0] != "github" {
		t.Errorf("Providers = %v, want [github]", entries[0].Providers)
	}
}

// provider が defaults で指定されるとそれが使われる
func TestResolveEntries_ProviderInheritFromDefaults(t *testing.T) {
	def := definition{}
	def.Defaults.Provider = &ProviderVal{Values: []string{"github"}}
	def.Variables = map[string]varConf{
		"FOO": {},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Providers) != 1 || entries[0].Providers[0] != "github" {
		t.Errorf("Providers = %v, want [github]", entries[0].Providers)
	}
}

// varConf.Provider が defaults.Provider より優先される
func TestResolveEntries_ProviderVarConfOverridesDefaults(t *testing.T) {
	def := definition{}
	def.Defaults.Provider = &ProviderVal{Values: []string{"github"}}
	def.Variables = map[string]varConf{
		"FOO": {Provider: &ProviderVal{Values: []string{"vercel"}}},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "github")
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Providers) != 1 || entries[0].Providers[0] != "vercel" {
		t.Errorf("Providers = %v, want [vercel]", entries[0].Providers)
	}
}

// provider に両方指定できる
func TestResolveEntries_ProviderBoth(t *testing.T) {
	pv := &ProviderVal{Values: []string{"vercel", "github"}}
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Provider: pv},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Providers) != 2 {
		t.Fatalf("Providers len = %d, want 2", len(entries[0].Providers))
	}
}

// provider に空白のみを指定すると dedup 後に空になりエラーを返す（静かに落ちない）
func TestResolveEntries_ProviderWhitespaceOnlyError(t *testing.T) {
	pv := &ProviderVal{Values: []string{" "}}
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Provider: pv},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	_, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err == nil {
		t.Error("空白のみの provider 値でエラーが返らなかった")
	}
}

// defaults.provider に不正値があれば varConf で上書きされていてもエラーを返す
func TestResolveEntries_DefaultsProviderInvalidAlwaysChecked(t *testing.T) {
	def := definition{}
	def.Defaults.Provider = &ProviderVal{Values: []string{"gitlab"}} // 不正値
	def.Variables = map[string]varConf{
		"FOO": {Provider: &ProviderVal{Values: []string{"vercel"}}}, // 上書きしても
	}
	envVars := map[string]string{"FOO": "bar"}
	_, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err == nil {
		t.Error("defaults.provider の不正値が varConf で上書きされても検証されるべき")
	}
}

// 不正な provider 値はエラーを返す
func TestResolveEntries_ProviderInvalid(t *testing.T) {
	pv := &ProviderVal{Values: []string{"gitlab"}}
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Provider: pv},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	_, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err == nil {
		t.Error("不正な provider 値でエラーが返らなかった")
	}
}

// provider が YAML に無ければ CLI フラグのデフォルトが使われる
func TestResolveEntries_ProviderFallbackToCLI(t *testing.T) {
	def := definition{
		Variables: map[string]varConf{
			"FOO": {},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "github")
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Providers) != 1 || entries[0].Providers[0] != "github" {
		t.Errorf("Providers = %v, want [github]", entries[0].Providers)
	}
}

// provider に重複値を指定すると重複排除され二重 Sync にならない
func TestResolveEntries_ProviderDuplicateDeduplication(t *testing.T) {
	pv := &ProviderVal{Values: []string{"vercel", "vercel"}}
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Provider: pv},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	entries, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err != nil {
		t.Fatalf("resolveEntries エラー: %v", err)
	}
	if len(entries[0].Providers) != 1 || entries[0].Providers[0] != "vercel" {
		t.Errorf("Providers = %v, want [vercel]（重複排除後）", entries[0].Providers)
	}
}

// varConf の provider に空配列を明示するとエラーを返す
func TestResolveEntries_ProviderExplicitEmptyArrayError(t *testing.T) {
	pv := &ProviderVal{Values: []string{}}
	def := definition{
		Variables: map[string]varConf{
			"FOO": {Provider: pv},
		},
	}
	envVars := map[string]string{"FOO": "bar"}
	_, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err == nil {
		t.Error("空配列の provider 指定でエラーが返らなかった")
	}
}

// defaults.provider に空配列を明示するとエラーを返す
func TestResolveEntries_ProviderDefaultsExplicitEmptyArrayError(t *testing.T) {
	def := definition{}
	def.Defaults.Provider = &ProviderVal{Values: []string{}}
	def.Variables = map[string]varConf{
		"FOO": {},
	}
	envVars := map[string]string{"FOO": "bar"}
	_, err := resolveEntries(def, envVars, []string{"FOO"}, "vercel")
	if err == nil {
		t.Error("defaults.provider に空配列を指定した場合にエラーが返らなかった")
	}
}
