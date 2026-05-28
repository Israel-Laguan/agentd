package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/kanban"
)

func TestConfigCommand_PrintsCronAndSchemaVersion(t *testing.T) {
	home := initHome(t)
	output := runCLI(t, home, "config")

	if !strings.Contains(output, "schema_version=") {
		t.Fatalf("output missing schema_version: %s", output)
	}
	if !strings.Contains(output, "cron.path=") {
		t.Fatalf("output missing cron.path: %s", output)
	}
}

func TestConfigCommand_WithSettings(t *testing.T) {
	home := initHome(t)

	cfg, err := config.Load(config.LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := kanban.OpenStore(cfg.DBPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer closeStore(store)

	ctx := context.Background()
	if err := store.SetSetting(ctx, "alpha", "one"); err != nil {
		t.Fatalf("SetSetting alpha: %v", err)
	}
	if err := store.SetSetting(ctx, "beta", "two"); err != nil {
		t.Fatalf("SetSetting beta: %v", err)
	}

	output := runCLI(t, home, "config")
	if !strings.Contains(output, "alpha=one") || !strings.Contains(output, "beta=two") {
		t.Fatalf("output missing settings: %s", output)
	}
	if strings.Contains(output, "no settings configured") {
		t.Fatalf("unexpected empty settings message: %s", output)
	}
}

func TestConfigShowCommand_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("gateway:\n  order: [horde]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("AGENTD_GATEWAY_ORDER=gemini\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if oldVal, had := os.LookupEnv("AGENTD_GATEWAY_ORDER"); had {
		_ = os.Unsetenv("AGENTD_GATEWAY_ORDER")
		t.Cleanup(func() { _ = os.Setenv("AGENTD_GATEWAY_ORDER", oldVal) })
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	output := runCLI(t, home, "config", "show")
	if !strings.Contains(output, "gateway.order=gemini") {
		t.Fatalf("output missing gateway.order: %s", output)
	}
	if !strings.Contains(output, "AGENTD_GATEWAY_ORDER") {
		t.Fatalf("output missing env source: %s", output)
	}
	if !strings.Contains(output, "overrides config.yaml") {
		t.Fatalf("output missing override note: %s", output)
	}
}

func TestConfigShowCommand_ProviderAPIKeyOverridesFile(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	config := `gateway:
  providers:
    - name: poolside
      adapter: openai
      api_key: sk-file
`
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("AGENTD_GATEWAY_POOLSIDE_API_KEY=sk-env\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if oldVal, had := os.LookupEnv("AGENTD_GATEWAY_POOLSIDE_API_KEY"); had {
		_ = os.Unsetenv("AGENTD_GATEWAY_POOLSIDE_API_KEY")
		t.Cleanup(func() { _ = os.Setenv("AGENTD_GATEWAY_POOLSIDE_API_KEY", oldVal) })
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })

	output := runCLI(t, home, "config", "show")
	if !strings.Contains(output, "gateway.providers[name=poolside].api_key") {
		t.Fatalf("output missing provider api_key line: %s", output)
	}
	if !strings.Contains(output, "AGENTD_GATEWAY_POOLSIDE_API_KEY") {
		t.Fatalf("output missing env source: %s", output)
	}
	if !strings.Contains(output, "overrides config.yaml") {
		t.Fatalf("output missing override note: %s", output)
	}
}

func TestConfigShow_ReadOnlyHome(t *testing.T) {
	home := initHome(t)
	if err := os.Chmod(home, 0o555); err != nil {
		t.Skipf("cannot chmod (may be running as root): %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	output := runCLI(t, home, "config", "show")
	if !strings.Contains(output, "api.address=") {
		t.Fatalf("config show on read-only home: missing resolved config: %s", output)
	}
}

func TestConfigShowCommand_NoOverrideWhenValuesAgree(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte("gateway:\n  order: [horde]\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("AGENTD_GATEWAY_ORDER", "horde")

	output := runCLI(t, home, "config", "show")
	if strings.Contains(output, "overrides config.yaml") {
		t.Fatalf("unexpected override note: %s", output)
	}
}
