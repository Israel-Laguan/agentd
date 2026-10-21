package config

import (
	"os"
	"path/filepath"
	"strconv"
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

func TestLoadToolchainPaths(t *testing.T) {
	dir := t.TempDir()
	cfg, err := loadWithYAML(t, "sandbox:\n  toolchain_paths: ["+strconv.Quote(dir)+"]\n")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Sandbox.ToolchainPaths) != 1 || cfg.Sandbox.ToolchainPaths[0] != dir {
		t.Fatalf("ToolchainPaths = %q, want [%q]", cfg.Sandbox.ToolchainPaths, dir)
	}
}

func TestLoadRejectsBadToolchainPaths(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	rootLink := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink("/", rootLink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative/go", "/", "/no/such/agentd-toolchain", file, rootLink} {
		_, err := loadWithYAML(t, "sandbox:\n  toolchain_paths: ["+strconv.Quote(path)+"]\n")
		if err == nil || !strings.Contains(err.Error(), "sandbox.toolchain_paths") {
			t.Errorf("Load(%q) error = %v, want a sandbox.toolchain_paths error", path, err)
		}
	}
}
