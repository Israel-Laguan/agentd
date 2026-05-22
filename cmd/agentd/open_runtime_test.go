package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
)

func TestOpenRuntime_NoProviders(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	if err := os.WriteFile(configPath, []byte("gateway:\n  order:\n    - openai\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AGENTD_GATEWAY_ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, _, _, cleanup, err := openRuntime(&rootOptions{home: home, configFile: configPath})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("openRuntime() error = %v, want nil (store-only path skips provider check)", err)
	}
}

func TestRequireStartupProviders_NoProviders(t *testing.T) {
	t.Setenv("AGENTD_GATEWAY_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("AGENTD_GATEWAY_ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	err := requireStartupProviders(config.GatewayConfig{Order: []string{"openai"}})
	if err == nil {
		t.Fatal("requireStartupProviders() error = nil, want no providers error")
	}
	if !strings.Contains(err.Error(), "no LLM providers available") {
		t.Fatalf("requireStartupProviders() error = %v", err)
	}
}

func TestOpenRuntime_NonWritableDirectory(t *testing.T) {
	home := initHome(t) // runs agentd init; sets AGENTD_GATEWAY_OPENAI_API_KEY=test-key

	if err := os.Chmod(home, 0o555); err != nil {
		t.Fatalf("chmod home: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	_, _, _, cleanup, err := openRuntime(&rootOptions{home: home})
	if cleanup != nil {
		defer cleanup()
	}
	if err == nil {
		t.Fatal("openRuntime() returned nil, want directory not writable error")
	}
	if !strings.Contains(err.Error(), "directory not writable") {
		t.Fatalf("error = %v, want 'directory not writable'", err)
	}
	summary, _ := describeCommandError(err)
	if !strings.Contains(summary, "data directories") {
		t.Fatalf("summary = %q, want mention of data directories", summary)
	}
}
