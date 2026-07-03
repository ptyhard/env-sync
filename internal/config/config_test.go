package config_test

import (
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ptyhard/env-sync/internal/config"
	_ "github.com/ptyhard/env-sync/internal/provider/github"
	_ "github.com/ptyhard/env-sync/internal/provider/vercel"
)

// ParseFlags のテスト

func TestParseFlags_Defaults(t *testing.T) {
	opts := config.ParseFlags([]string{}, nil, nil)
	if opts.Env != ".env" {
		t.Errorf("デフォルト Env: got %q, want .env", opts.Env)
	}
	if opts.Def != "env-sync.yaml" {
		t.Errorf("デフォルト Def: got %q, want env-sync.yaml", opts.Def)
	}
	if opts.DryRun {
		t.Error("デフォルト DryRun: got true, want false")
	}
	if opts.Yes {
		t.Error("デフォルト Yes: got true, want false")
	}
}

func TestParseFlags_EnvFlag(t *testing.T) {
	opts := config.ParseFlags([]string{"--env", ".env.production"}, nil, nil)
	if opts.Env != ".env.production" {
		t.Errorf("--env: got %q, want .env.production", opts.Env)
	}
}

func TestParseFlags_EnvFlagEquals(t *testing.T) {
	opts := config.ParseFlags([]string{"--env=.env.staging"}, nil, nil)
	if opts.Env != ".env.staging" {
		t.Errorf("--env=: got %q, want .env.staging", opts.Env)
	}
}

func TestParseFlags_DefFlag(t *testing.T) {
	opts := config.ParseFlags([]string{"--def", "custom.yaml"}, nil, nil)
	if opts.Def != "custom.yaml" {
		t.Errorf("--def: got %q, want custom.yaml", opts.Def)
	}
}

func TestParseFlags_DryRun(t *testing.T) {
	opts := config.ParseFlags([]string{"--dry-run"}, nil, nil)
	if !opts.DryRun {
		t.Error("--dry-run: got false, want true")
	}
}

func TestParseFlags_Yes(t *testing.T) {
	for _, arg := range []string{"--yes", "-yes", "-y"} {
		opts := config.ParseFlags([]string{arg}, nil, nil)
		if !opts.Yes {
			t.Errorf("%s: got false, want true", arg)
		}
	}
}

func TestParseFlags_DefaultProvider(t *testing.T) {
	opts := config.ParseFlags([]string{}, nil, nil)
	if opts.Provider != "vercel" {
		t.Errorf("Provider のデフォルト = %q, want %q", opts.Provider, "vercel")
	}
}

func TestParseFlags_ProviderVercel(t *testing.T) {
	opts := config.ParseFlags([]string{"--provider", "vercel"}, nil, nil)
	if opts.Provider != "vercel" {
		t.Errorf("Provider = %q, want vercel", opts.Provider)
	}
}

func TestParseFlags_ProviderGitHub(t *testing.T) {
	opts := config.ParseFlags([]string{"--provider", "github"}, nil, nil)
	if opts.Provider != "github" {
		t.Errorf("Provider = %q, want github", opts.Provider)
	}
}

func TestParseFlags_ProviderEqualForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--provider=github"}, nil, nil)
	if opts.Provider != "github" {
		t.Errorf("--provider=github が解析されない: got %q", opts.Provider)
	}
}

// TestParseFlags_VersionFn は --version/-version のとき versionFn が呼ばれることを確認する。
// os.Exit を避けるため versionFn の中でパニックを起こし、recover で捕捉する。
func TestParseFlags_VersionFn_Called(t *testing.T) {
	for _, arg := range []string{"--version", "-version"} {
		arg := arg
		t.Run(arg, func(t *testing.T) {
			called := false
			// os.Exit を直接呼ぶため goroutine + recover でテストする。
			// versionFn が呼ばれたことを記録し、os.Exit(0) の代わりに panic でフロー中断。
			done := make(chan bool, 1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						done <- called
					} else {
						done <- called
					}
				}()
				config.ParseFlags([]string{arg}, nil, func() {
					called = true
					panic("stop-before-os-exit") // os.Exit(0) に到達させない
				})
				done <- called
			}()
			if got := <-done; !got {
				t.Errorf("%s: versionFn が呼ばれなかった", arg)
			}
		})
	}
}

// ProviderVal の YAML パーステスト

func TestProviderVal_UnmarshalYAML_String(t *testing.T) {
	type T struct {
		Provider *config.ProviderVal `yaml:"provider"`
	}
	var v T
	if err := yaml.Unmarshal([]byte("provider: vercel"), &v); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	if v.Provider == nil || len(v.Provider.Values) != 1 || v.Provider.Values[0] != "vercel" {
		t.Errorf("ProviderVal = %v, want [vercel]", v.Provider)
	}
}

func TestProviderVal_UnmarshalYAML_Sequence(t *testing.T) {
	type T struct {
		Provider *config.ProviderVal `yaml:"provider"`
	}
	var v T
	if err := yaml.Unmarshal([]byte("provider: [vercel, github]"), &v); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	if v.Provider == nil || len(v.Provider.Values) != 2 {
		t.Fatalf("ProviderVal.Values len = %d, want 2", len(v.Provider.Values))
	}
	if v.Provider.Values[0] != "vercel" || v.Provider.Values[1] != "github" {
		t.Errorf("ProviderVal.Values = %v, want [vercel github]", v.Provider.Values)
	}
}

func TestProviderVal_UnmarshalYAML_Nil(t *testing.T) {
	type T struct {
		Provider *config.ProviderVal `yaml:"provider"`
	}
	var v T
	if err := yaml.Unmarshal([]byte("secret: true"), &v); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	if v.Provider != nil {
		t.Errorf("provider フィールド未指定なのに非 nil: %v", v.Provider)
	}
}

// --- --vercel-project / --github-repo フラグのテスト ---

func TestParseFlags_VercelProject_SpaceForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--vercel-project", "app-a"}, nil, nil)
	if opts.VercelProject != "app-a" {
		t.Errorf("VercelProject = %q, want app-a", opts.VercelProject)
	}
}

func TestParseFlags_VercelProject_EqualForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--vercel-project=app-b"}, nil, nil)
	if opts.VercelProject != "app-b" {
		t.Errorf("VercelProject = %q, want app-b", opts.VercelProject)
	}
}

func TestParseFlags_GitHubRepo_SpaceForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--github-repo", "frontend"}, nil, nil)
	if opts.GitHubRepo != "frontend" {
		t.Errorf("GitHubRepo = %q, want frontend", opts.GitHubRepo)
	}
}

func TestParseFlags_GitHubRepo_EqualForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--github-repo=backend"}, nil, nil)
	if opts.GitHubRepo != "backend" {
		t.Errorf("GitHubRepo = %q, want backend", opts.GitHubRepo)
	}
}

func TestParseFlags_VercelProject_Default(t *testing.T) {
	opts := config.ParseFlags([]string{}, nil, nil)
	if opts.VercelProject != "" {
		t.Errorf("VercelProject のデフォルト = %q, want empty", opts.VercelProject)
	}
}

func TestParseFlags_GitHubRepo_Default(t *testing.T) {
	opts := config.ParseFlags([]string{}, nil, nil)
	if opts.GitHubRepo != "" {
		t.Errorf("GitHubRepo のデフォルト = %q, want empty", opts.GitHubRepo)
	}
}

// --- VarConf.VercelProject の YAML パーステスト ---

func TestVarConf_VercelProject_String(t *testing.T) {
	yaml := `
variables:
  API_URL:
    vercel_project: app-a
`
	var def config.Definition
	if err := unmarshalDefinition(yaml, &def); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	conf := def.Variables["API_URL"]
	if conf.VercelProject == nil {
		t.Fatal("VercelProject が nil")
	}
	if len(conf.VercelProject.Values) != 1 || conf.VercelProject.Values[0] != "app-a" {
		t.Errorf("VercelProject.Values = %v, want [app-a]", conf.VercelProject.Values)
	}
}

func TestVarConf_VercelProject_Sequence(t *testing.T) {
	yaml := `
variables:
  DB_URL:
    vercel_project: [app-a, app-b]
`
	var def config.Definition
	if err := unmarshalDefinition(yaml, &def); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	conf := def.Variables["DB_URL"]
	if conf.VercelProject == nil {
		t.Fatal("VercelProject が nil")
	}
	if len(conf.VercelProject.Values) != 2 {
		t.Fatalf("VercelProject.Values len = %d, want 2", len(conf.VercelProject.Values))
	}
	if conf.VercelProject.Values[0] != "app-a" || conf.VercelProject.Values[1] != "app-b" {
		t.Errorf("VercelProject.Values = %v, want [app-a app-b]", conf.VercelProject.Values)
	}
}

func TestVarConf_VercelProject_Nil(t *testing.T) {
	yaml := `
variables:
  FOO:
    secret: true
`
	var def config.Definition
	if err := unmarshalDefinition(yaml, &def); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	conf := def.Variables["FOO"]
	if conf.VercelProject != nil {
		t.Errorf("vercel_project 未指定なのに非 nil: %v", conf.VercelProject)
	}
}

func TestDefinition_Defaults_VercelProject(t *testing.T) {
	yaml := `
defaults:
  vercel_project: app-a
variables:
  FOO: {}
`
	var def config.Definition
	if err := unmarshalDefinition(yaml, &def); err != nil {
		t.Fatalf("Unmarshal エラー: %v", err)
	}
	if def.Defaults.VercelProject == nil {
		t.Fatal("Defaults.VercelProject が nil")
	}
	if len(def.Defaults.VercelProject.Values) != 1 || def.Defaults.VercelProject.Values[0] != "app-a" {
		t.Errorf("Defaults.VercelProject.Values = %v, want [app-a]", def.Defaults.VercelProject.Values)
	}
}

func unmarshalDefinition(src string, def *config.Definition) error {
	return yaml.Unmarshal([]byte(src), def)
}

// --- --language フラグ（--lang のエイリアス）のテスト ---

// TestParseFlags_Language_SpaceForm は --language ja がスペース区切りで解析されることを確認する。
func TestParseFlags_Language_SpaceForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--language", "ja"}, nil, nil)
	if opts.Language != "ja" {
		t.Errorf("--language ja: Language = %q, want ja", opts.Language)
	}
}

// TestParseFlags_Language_EqualForm は --language=ja がイコール形式で解析されることを確認する。
func TestParseFlags_Language_EqualForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--language=ja"}, nil, nil)
	if opts.Language != "ja" {
		t.Errorf("--language=ja: Language = %q, want ja", opts.Language)
	}
}

// --- PrescanLang のテスト ---

// TestPrescanLang_LangFlag_SpaceForm は --lang val 形式でプレスキャンできることを確認する。
func TestPrescanLang_LangFlag_SpaceForm(t *testing.T) {
	got := config.PrescanLang([]string{"--lang", "ja", "--env", ".env"})
	if got != "ja" {
		t.Errorf("PrescanLang(--lang ja ...): got %q, want ja", got)
	}
}

// TestPrescanLang_LangFlag_EqualForm は --lang=val 形式でプレスキャンできることを確認する。
func TestPrescanLang_LangFlag_EqualForm(t *testing.T) {
	got := config.PrescanLang([]string{"--lang=ja"})
	if got != "ja" {
		t.Errorf("PrescanLang(--lang=ja): got %q, want ja", got)
	}
}

// TestPrescanLang_LanguageFlag_SpaceForm は --language val 形式でプレスキャンできることを確認する。
func TestPrescanLang_LanguageFlag_SpaceForm(t *testing.T) {
	got := config.PrescanLang([]string{"--language", "ja"})
	if got != "ja" {
		t.Errorf("PrescanLang(--language ja): got %q, want ja", got)
	}
}

// TestPrescanLang_LanguageFlag_EqualForm は --language=val 形式でプレスキャンできることを確認する。
func TestPrescanLang_LanguageFlag_EqualForm(t *testing.T) {
	got := config.PrescanLang([]string{"--language=en"})
	if got != "en" {
		t.Errorf("PrescanLang(--language=en): got %q, want en", got)
	}
}

// TestPrescanLang_NoFlag は言語フラグがないとき空文字を返すことを確認する。
func TestPrescanLang_NoFlag(t *testing.T) {
	got := config.PrescanLang([]string{"--env", ".env", "--dry-run"})
	if got != "" {
		t.Errorf("PrescanLang(フラグなし): got %q, want empty", got)
	}
}

// TestPrescanLang_Empty は空スライスのとき空文字を返すことを確認する。
func TestPrescanLang_Empty(t *testing.T) {
	got := config.PrescanLang([]string{})
	if got != "" {
		t.Errorf("PrescanLang(空): got %q, want empty", got)
	}
}

// --environments フラグのテスト

// TestParseFlags_Environments_CommaSeparated はカンマ区切りで複数の環境名を解析できること。
func TestParseFlags_Environments_CommaSeparated(t *testing.T) {
	opts := config.ParseFlags([]string{"--environments", "staging,preview"}, nil, nil)
	want := []string{"staging", "preview"}
	if len(opts.Environments) != len(want) {
		t.Fatalf("Environments len = %d, want %d: %v", len(opts.Environments), len(want), opts.Environments)
	}
	for i, e := range want {
		if opts.Environments[i] != e {
			t.Errorf("Environments[%d] = %q, want %q", i, opts.Environments[i], e)
		}
	}
}

// TestParseFlags_Environments_EqualForm は --environments=staging,preview の形式を解析できること。
func TestParseFlags_Environments_EqualForm(t *testing.T) {
	opts := config.ParseFlags([]string{"--environments=production"}, nil, nil)
	if len(opts.Environments) != 1 || opts.Environments[0] != "production" {
		t.Errorf("Environments = %v, want [production]", opts.Environments)
	}
}

// TestParseFlags_Environments_TrimmedSpaces はカンマ区切り値の前後スペースをトリムすること。
func TestParseFlags_Environments_TrimmedSpaces(t *testing.T) {
	opts := config.ParseFlags([]string{"--environments", " production , preview "}, nil, nil)
	want := []string{"production", "preview"}
	if len(opts.Environments) != len(want) {
		t.Fatalf("Environments = %v, want %v", opts.Environments, want)
	}
	for i, e := range want {
		if opts.Environments[i] != e {
			t.Errorf("Environments[%d] = %q, want %q", i, opts.Environments[i], e)
		}
	}
}

// TestParseFlags_Environments_ShortForm は -environments でも動作すること。
func TestParseFlags_Environments_ShortForm(t *testing.T) {
	opts := config.ParseFlags([]string{"-environments", "staging"}, nil, nil)
	if len(opts.Environments) != 1 || opts.Environments[0] != "staging" {
		t.Errorf("Environments = %v, want [staging]", opts.Environments)
	}
}

// TestParseFlags_Environments_EmptyDefault はフラグ未指定のとき Environments が nil であること。
func TestParseFlags_Environments_EmptyDefault(t *testing.T) {
	opts := config.ParseFlags([]string{}, nil, nil)
	if len(opts.Environments) != 0 {
		t.Errorf("Environments のデフォルト: got %v, want nil/空", opts.Environments)
	}
}
