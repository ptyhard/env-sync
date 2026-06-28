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
