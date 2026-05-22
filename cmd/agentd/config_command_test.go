package main

import (
	"context"
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
