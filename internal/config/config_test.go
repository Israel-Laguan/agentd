package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveHome(t *testing.T) {
	origEnv := os.Getenv("AGENTD_HOME")
	defer os.Setenv("AGENTD_HOME", origEnv)

	t.Run("Override", func(t *testing.T) {
		home, err := ResolveHome("/tmp/custom")
		if err != nil {
			t.Fatalf("ResolveHome() error = %v", err)
		}
		want, _ := filepath.Abs("/tmp/custom")
		if home != want {
			t.Errorf("got %q, want %q", home, want)
		}
	})

	t.Run("EnvVar", func(t *testing.T) {
		os.Setenv("AGENTD_HOME", "/tmp/env")
		home, err := ResolveHome("")
		if err != nil {
			t.Fatalf("ResolveHome() error = %v", err)
		}
		want, _ := filepath.Abs("/tmp/env")
		if home != want {
			t.Errorf("got %q, want %q", home, want)
		}
	})

	t.Run("Default", func(t *testing.T) {
		os.Unsetenv("AGENTD_HOME")
		home, err := ResolveHome("")
		if err != nil {
			t.Fatalf("ResolveHome() error = %v", err)
		}
		userHome, _ := os.UserHomeDir()
		want := filepath.Join(userHome, defaultHomeDirName)
		if home != want {
			t.Errorf("got %q, want %q", home, want)
		}
	})
}

func TestEnsureDirs(t *testing.T) {
	tmp := t.TempDir()
	cfg := Config{
		HomeDir:     filepath.Join(tmp, "home"),
		ProjectsDir: filepath.Join(tmp, "projects"),
		UploadsDir:  filepath.Join(tmp, "uploads"),
		ArchivesDir: filepath.Join(tmp, "archives"),
	}

	if err := EnsureDirs(cfg); err != nil {
		t.Fatalf("EnsureDirs() error = %v", err)
	}

	for _, dir := range []string{cfg.HomeDir, cfg.ProjectsDir, cfg.UploadsDir, cfg.ArchivesDir} {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			t.Errorf("directory %s was not created", dir)
		}
	}
}
