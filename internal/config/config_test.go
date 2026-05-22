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

func TestLoad_AGENTD_HOME_FromCwdDotEnv(t *testing.T) {
	tmp := t.TempDir()
	customHome := filepath.Join(tmp, "custom-agentd")
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AGENTD_HOME="+customHome+"\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	_ = os.Unsetenv("AGENTD_HOME")

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != customHome {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, customHome)
	}
	wantDB := filepath.Join(customHome, "global.db")
	if cfg.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q", cfg.DBPath, wantDB)
	}
	wantProjects := filepath.Join(customHome, "projects")
	if cfg.ProjectsDir != wantProjects {
		t.Errorf("ProjectsDir = %q, want %q", cfg.ProjectsDir, wantProjects)
	}
}

func TestLoad_MalformedDotEnv(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("KEY=\"unterminated\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}

	_, err = Load(LoadOptions{})
	if err == nil {
		t.Fatal("Load() error = nil, want malformed .env error")
	}
	if !strings.Contains(err.Error(), "load .env from current directory") {
		t.Fatalf("Load() error = %v, want current-directory .env wrapper", err)
	}
}

func TestLoad_ProcessEnvOverridesHomeDotEnv(t *testing.T) {
	tmp := t.TempDir()
	processHome := filepath.Join(tmp, "process-home")
	fileHome := filepath.Join(tmp, "file-home")
	if err := os.MkdirAll(processHome, 0o755); err != nil {
		t.Fatalf("mkdir process-home: %v", err)
	}
	if err := os.WriteFile(filepath.Join(processHome, ".env"), []byte("AGENTD_HOME="+fileHome+"\n"), 0o644); err != nil {
		t.Fatalf("write home .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	t.Setenv("AGENTD_HOME", processHome)

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != processHome {
		t.Errorf("HomeDir = %q, want %q (process env should win over home .env)", cfg.HomeDir, processHome)
	}
	wantDB := filepath.Join(processHome, "global.db")
	if cfg.DBPath != wantDB {
		t.Errorf("DBPath = %q, want %q (derived paths should follow effective home)", cfg.DBPath, wantDB)
	}
}

func TestLoad_HomeDotEnvAfterAGENTD_HOMEOverride(t *testing.T) {
	tmp := t.TempDir()
	homeB := filepath.Join(tmp, "home-b")
	homeC := filepath.Join(tmp, "home-c")
	if err := os.MkdirAll(homeC, 0o755); err != nil {
		t.Fatalf("mkdir home-c: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".env"), []byte("AGENTD_HOME="+homeB+"\n"), 0o644); err != nil {
		t.Fatalf("write cwd .env: %v", err)
	}
	if err := os.MkdirAll(homeB, 0o755); err != nil {
		t.Fatalf("mkdir home-b: %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeB, ".env"), []byte("AGENTD_HOME="+homeC+"\n"), 0o644); err != nil {
		t.Fatalf("write home-b .env: %v", err)
	}
	const marker = "MARKER_FROM_HOME_C"
	if err := os.WriteFile(filepath.Join(homeC, ".env"), []byte(marker+"=loaded\n"), 0o644); err != nil {
		t.Fatalf("write home-c .env: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if old, ok := os.LookupEnv("AGENTD_HOME"); ok {
		t.Cleanup(func() { _ = os.Setenv("AGENTD_HOME", old) })
	} else {
		t.Cleanup(func() { _ = os.Unsetenv("AGENTD_HOME") })
	}
	_ = os.Unsetenv("AGENTD_HOME")
	t.Cleanup(func() { _ = os.Unsetenv(marker) })
	_ = os.Unsetenv(marker)

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HomeDir != homeC {
		t.Errorf("HomeDir = %q, want %q", cfg.HomeDir, homeC)
	}
	if got := os.Getenv(marker); got != "loaded" {
		t.Errorf("%s = %q, want loaded", marker, got)
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
