package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ConfigReferenceYAML(t *testing.T) {
	root := repoRoot(t)
	ref := filepath.Join(root, "config.reference.yaml")
	homeDir := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}

	cfg, err := Load(LoadOptions{HomeOverride: homeDir, ConfigFile: ref})
	if err != nil {
		t.Fatalf("Load(config.reference.yaml) error = %v", err)
	}
	if _, err := cfg.Gateway.ProviderConfigs(); err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}

	g := cfg.Gateway.Gemini
	if g.BaseURL == "" {
		t.Error("gateway.gemini.base_url is empty")
	}
	if g.Model == "" {
		t.Error("gateway.gemini.model is empty")
	}
	if g.Timeout <= 0 {
		t.Errorf("gateway.gemini.timeout = %v, want positive duration", g.Timeout)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root (go.mod) not found")
		}
		dir = parent
	}
}
