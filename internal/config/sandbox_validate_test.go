package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadWithYAML(t *testing.T, body string) (Config, error) {
	t.Helper()
	home := filepath.Join(t.TempDir(), "agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	path := filepath.Join(t.TempDir(), "agentd.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return Load(LoadOptions{HomeOverride: home, ConfigFile: path})
}

// T-050: a typo in a safety setting must stop the daemon at load instead of
// silently weakening the sandbox.
func TestLoadRejectsUnknownSandboxBackend(t *testing.T) {
	_, err := loadWithYAML(t, "sandbox:\n  backend: bubblewarp\n")
	if err == nil || !strings.Contains(err.Error(), "sandbox.backend") || !strings.Contains(err.Error(), "bubblewarp") {
		t.Fatalf("Load() error = %v, want a sandbox.backend error naming the bad value", err)
	}
}

func TestLoadRejectsInvalidScrubPattern(t *testing.T) {
	_, err := loadWithYAML(t, "sandbox:\n  scrub_patterns:\n    - 'ok-[a-z]+'\n    - '(unclosed'\n")
	if err == nil || !strings.Contains(err.Error(), "sandbox.scrub_patterns") || !strings.Contains(err.Error(), "(unclosed") {
		t.Fatalf("Load() error = %v, want a sandbox.scrub_patterns error naming the bad pattern", err)
	}
}

func TestLoadAcceptsKnownSandboxSettings(t *testing.T) {
	cfg, err := loadWithYAML(t, "sandbox:\n  backend: Filter-Only\n  scrub_patterns:\n    - 'tok-[a-z]+'\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Sandbox.ScrubPatterns) != 1 {
		t.Fatalf("ScrubPatterns = %v", cfg.Sandbox.ScrubPatterns)
	}
}
