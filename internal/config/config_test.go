package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHome_WithOverride(t *testing.T) {
	home, err := ResolveHome("/custom/path")
	if err != nil {
		t.Fatalf("ResolveHome() error = %v", err)
	}
	if home != "/custom/path" {
		t.Errorf("ResolveHome() = %v, want /custom/path", home)
	}
}

func TestResolveHome_WithEnvVar(t *testing.T) {
	t.Setenv("AGENTD_HOME", "/env/path")
	home, err := ResolveHome("")
	if err != nil {
		t.Fatalf("ResolveHome() error = %v", err)
	}
	if home != "/env/path" {
		t.Errorf("ResolveHome() = %v, want /env/path", home)
	}
}

func TestResolveHome_UsesDefault(t *testing.T) {
	home, err := ResolveHome("")
	if err != nil {
		t.Fatalf("ResolveHome() error = %v", err)
	}
	expected := filepath.Join(os.Getenv("HOME"), defaultHomeDirName)
	if home != expected {
		t.Errorf("ResolveHome() = %v, want %v", home, expected)
	}
}

func TestBaseConfig(t *testing.T) {
	cfg := baseConfig("/test/home")
	if cfg.HomeDir != "/test/home" {
		t.Errorf("HomeDir = %v, want /test/home", cfg.HomeDir)
	}
	if cfg.DBPath != "/test/home/global.db" {
		t.Errorf("DBPath = %v, want /test/home/global.db", cfg.DBPath)
	}
	if cfg.ProjectsDir != "/test/home/projects" {
		t.Errorf("ProjectsDir = %v, want /test/home/projects", cfg.ProjectsDir)
	}
	if cfg.UploadsDir != "/test/home/uploads" {
		t.Errorf("UploadsDir = %v, want /test/home/uploads", cfg.UploadsDir)
	}
	if cfg.ArchivesDir != "/test/home/archives" {
		t.Errorf("ArchivesDir = %v, want /test/home/archives", cfg.ArchivesDir)
	}
	if cfg.CronPath != "/test/home/agentd.crontab" {
		t.Errorf("CronPath = %v, want /test/home/agentd.crontab", cfg.CronPath)
	}
}

func TestEnsureDirs(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		HomeDir:     filepath.Join(tmp, "agentd"),
		ProjectsDir: filepath.Join(tmp, "agentd", "projects"),
		UploadsDir:  filepath.Join(tmp, "agentd", "uploads"),
		ArchivesDir: filepath.Join(tmp, "agentd", "archives"),
	}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if _, err := os.Stat(cfg.HomeDir); err != nil {
		t.Errorf("HomeDir not created: %v", err)
	}
	if _, err := os.Stat(cfg.ProjectsDir); err != nil {
		t.Errorf("ProjectsDir not created: %v", err)
	}
	if _, err := os.Stat(cfg.UploadsDir); err != nil {
		t.Errorf("UploadsDir not created: %v", err)
	}
	if _, err := os.Stat(cfg.ArchivesDir); err != nil {
		t.Errorf("ArchivesDir not created: %v", err)
	}
}

func TestEnsureDirs_AlreadyExists(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		HomeDir:     filepath.Join(tmp, "agentd"),
		ProjectsDir: filepath.Join(tmp, "agentd", "projects"),
		UploadsDir:  filepath.Join(tmp, "agentd", "uploads"),
		ArchivesDir: filepath.Join(tmp, "agentd", "archives"),
	}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("EnsureDirs() second call error = %v", err)
	}
}

func TestEnsureDirs_CreatesParent(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		HomeDir:     filepath.Join(tmp, "a", "b", "c"),
		ProjectsDir: filepath.Join(tmp, "a", "b", "c", "projects"),
		UploadsDir:  filepath.Join(tmp, "a", "b", "c", "uploads"),
		ArchivesDir: filepath.Join(tmp, "a", "b", "c", "archives"),
	}
	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}
	if _, err := os.Stat(cfg.HomeDir); err != nil {
		t.Errorf("Nested HomeDir not created: %v", err)
	}
}

func TestLoad_WithMissingConfig(t *testing.T) {
	tmp := t.TempDir()
	homeDir := filepath.Join(tmp, "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("failed to create home dir: %v", err)
	}

	cfg, err := Load(LoadOptions{HomeOverride: homeDir})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != homeDir {
		t.Errorf("HomeDir = %v, want %v", cfg.HomeDir, homeDir)
	}
	if cfg.API.Address != defaultAPIAddress {
		t.Errorf("API.Address = %v, want %v", cfg.API.Address, defaultAPIAddress)
	}
	wantSkills := filepath.Join(homeDir, DefaultSkillsGlobalDir)
	if cfg.Queue.Skills.GlobalDir != wantSkills {
		t.Errorf("Queue.Skills.GlobalDir = %q, want %q", cfg.Queue.Skills.GlobalDir, wantSkills)
	}
}

func TestLoad_SkillsGlobalDir_ExplicitAbsolute(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	abs := filepath.Join(t.TempDir(), "explicit-global-skills")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := fmt.Sprintf("queue:\n  skills:\n    global_dir: %q\n", filepath.ToSlash(abs))
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Queue.Skills.GlobalDir != abs {
		t.Fatalf("Queue.Skills.GlobalDir = %q, want %q", cfg.Queue.Skills.GlobalDir, abs)
	}
}

func TestLoad_AuditPath_ResolvesRelative(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := "agentic:\n  audit:\n    enabled: true\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := filepath.Join(homeDir, "audit.jsonl")
	if cfg.Agentic.Audit.Path != want {
		t.Fatalf("Agentic.Audit.Path = %q, want %q", cfg.Agentic.Audit.Path, want)
	}
}

func TestLoad_AuditPath_AbsoluteUnchanged(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	abs := filepath.Join(t.TempDir(), "var", "log", "agentd", "audit.jsonl")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := fmt.Sprintf("agentic:\n  audit:\n    enabled: true\n    path: %q\n", filepath.ToSlash(abs))
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Agentic.Audit.Path != abs {
		t.Fatalf("Agentic.Audit.Path = %q, want %q", cfg.Agentic.Audit.Path, abs)
	}
}

func TestResolveSkillsGlobalDir(t *testing.T) {
	home := filepath.Join(t.TempDir(), "agentd-home")
	t.Run("empty", func(t *testing.T) {
		if got := resolveSkillsGlobalDir(home, ""); got != "" {
			t.Errorf("resolveSkillsGlobalDir() = %q, want empty", got)
		}
	})
	t.Run("relative_default", func(t *testing.T) {
		got := resolveSkillsGlobalDir(home, DefaultSkillsGlobalDir)
		want := filepath.Join(home, DefaultSkillsGlobalDir)
		if got != want {
			t.Errorf("resolveSkillsGlobalDir() = %q, want %q", got, want)
		}
	})
	t.Run("nested_relative", func(t *testing.T) {
		got := resolveSkillsGlobalDir(home, filepath.Join("nested", "skills"))
		want := filepath.Join(home, "nested", "skills")
		if got != want {
			t.Errorf("resolveSkillsGlobalDir() = %q, want %q", got, want)
		}
	})
	t.Run("absolute", func(t *testing.T) {
		abs := filepath.Join(t.TempDir(), "abs-skills-only")
		if got := resolveSkillsGlobalDir(home, abs); got != abs {
			t.Errorf("resolveSkillsGlobalDir() = %q, want %q", got, abs)
		}
	})
	t.Run("tilde_prefix", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		got := resolveSkillsGlobalDir(home, "~/.agentd/skills")
		want := filepath.Join(os.Getenv("HOME"), ".agentd", "skills")
		if got != want {
			t.Errorf("resolveSkillsGlobalDir() = %q, want %q", got, want)
		}
	})
}

func TestLoad_GatewayProviders_UnmarshalError(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := "gateway:\n  providers: not-a-list\n"
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
	if err == nil {
		t.Fatal("Load() error = nil, want gateway.providers unmarshal error")
	}
	if !strings.Contains(err.Error(), "unmarshal gateway.providers") {
		t.Fatalf("Load() error = %v, want unmarshal gateway.providers", err)
	}
}

func TestLoad_GatewayOrder_DuplicateEntry(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := `gateway:
  order: [openai, openai]
  openai:
    api_key: sk-test
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
	if err == nil {
		t.Fatal("Load() error = nil, want duplicate gateway.order error")
	}
	if !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "gateway.order") {
		t.Fatalf("Load() error = %v, want duplicate error in gateway.order", err)
	}
}

func TestLoad_GatewayProviders_CustomProvider(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := `gateway:
  order: [poolside]
  providers:
    - name: poolside
      adapter: openai
      base_url: "https://inference.poolside.ai/v1"
      model: "poolside/laguna-m.1"
      api_key_env: POOLSIDE_API_KEY
      health: api_key
      capabilities:
        chat_tools: true
`
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Run("provider_configs", func(t *testing.T) {
		cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		configs, err := cfg.Gateway.ProviderConfigs()
		if err != nil {
			t.Fatalf("ProviderConfigs() error = %v", err)
		}
		if len(configs) != 1 {
			t.Fatalf("ProviderConfigs() length = %d, want 1", len(configs))
		}
		if configs[0].Name != "poolside" || configs[0].Type != "openai" {
			t.Fatalf("ProviderConfigs()[0] = %+v, want poolside/openai", configs[0])
		}
		if configs[0].BaseURL != "https://inference.poolside.ai/v1" {
			t.Fatalf("BaseURL = %q, want poolside inference URL", configs[0].BaseURL)
		}
		if configs[0].Model != "poolside/laguna-m.1" {
			t.Fatalf("Model = %q, want poolside/laguna-m.1", configs[0].Model)
		}
	})

	t.Run("api_key_env", func(t *testing.T) {
		t.Setenv("POOLSIDE_API_KEY", "test-key")
		cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(cfg.Gateway.Providers) != 1 {
			t.Fatalf("Providers length = %d, want 1", len(cfg.Gateway.Providers))
		}
		if cfg.Gateway.Providers[0].APIKey != "test-key" {
			t.Fatalf("APIKey = %q, want test-key from POOLSIDE_API_KEY", cfg.Gateway.Providers[0].APIKey)
		}
	})
}

func TestLoad_ConfigFileCannotOverrideHome(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	otherHome := filepath.Join(t.TempDir(), "other-home")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	body := fmt.Sprintf("home: %q\n", filepath.ToSlash(otherHome))
	if err := os.WriteFile(configPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != homeDir {
		t.Fatalf("HomeDir = %q, want %q (config home: must not override resolved home)", cfg.HomeDir, homeDir)
	}
}

func TestLoad_ConcurrentSafe(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	for _, dir := range []string{dirA, dirB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	errCh := make(chan error, 2)
	go func() {
		_, err := Load(LoadOptions{HomeOverride: dirA})
		errCh <- err
	}()
	go func() {
		_, err := Load(LoadOptions{HomeOverride: dirB})
		errCh <- err
	}()
	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	}
}

func TestIsConfigNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ConfigFileNotFoundError", &os.PathError{}, false},
		{"Nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isConfigNotFound(tt.err)
			if got != tt.want {
				t.Errorf("isConfigNotFound() = %v, want %v", got, tt.want)
			}
		})
	}
}
