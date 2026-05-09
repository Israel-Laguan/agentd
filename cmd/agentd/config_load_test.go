package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/config"
)

func TestExplicitConfigOverridesEnv(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	writeConfigTestFile(t, configPath, []byte("api:\n  address: 127.0.0.1:7777\n"))
	t.Setenv("AGENTD_API_ADDRESS", "127.0.0.1:9999")

	cfg, err := config.Load(config.LoadOptions{
		HomeOverride: home,
		ConfigFile:   configPath,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.Address != "127.0.0.1:7777" {
		t.Fatalf("API.Address = %q, want explicit config value", cfg.API.Address)
	}
}

func TestEnvOverridesAutoDiscoveredConfig(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	writeConfigTestFile(t, filepath.Join(home, "config.yaml"), []byte("api:\n  address: 127.0.0.1:7777\n"))
	t.Setenv("AGENTD_API_ADDRESS", "127.0.0.1:9999")

	cfg, err := config.Load(config.LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.Address != "127.0.0.1:9999" {
		t.Fatalf("API.Address = %q, want env value", cfg.API.Address)
	}
}

func TestMissingExplicitConfigReturnsError(t *testing.T) {
	_, err := config.Load(config.LoadOptions{
		HomeOverride: filepath.Join(t.TempDir(), ".agentd"),
		ConfigFile:   filepath.Join(t.TempDir(), "missing.yaml"),
	})
	if err == nil {
		t.Fatal("Load() error = nil, want missing explicit config error")
	}
}

func TestMaterializeTokenFromExplicitConfig(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	configPath := filepath.Join(t.TempDir(), "agentd.yaml")
	writeConfigTestFile(t, configPath, []byte("api:\n  address: 127.0.0.1:7777\n  materialize_token: \"rotate-me\"\n"))
	cfg, err := config.Load(config.LoadOptions{
		HomeOverride: home,
		ConfigFile:   configPath,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.API.MaterializeToken != "rotate-me" {
		t.Fatalf("API.MaterializeToken = %q, want rotate-me", cfg.API.MaterializeToken)
	}
}

func writeConfigTestFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write config file: %v", err)
	}
}

func TestWriteConfigOutputsExpectedValues(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	cfg, err := config.Load(config.LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	var buf bytes.Buffer
	if err := writeConfig(&buf, cfg); err != nil {
		t.Fatalf("writeConfig() error = %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("home="+home)) {
		t.Errorf("output missing home, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("db_path="+cfg.DBPath)) {
		t.Errorf("output missing db_path, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("api.address="+cfg.API.Address)) {
		t.Errorf("output missing api.address, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("cron.task-dispatch=@every")) {
		t.Errorf("output missing cron.task-dispatch, got: %s", output)
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"ab", "****"},
		{"abcd", "****"},
		{"abcdef123456", "abcd****123456"},
		{"secret-token", "secr****-token"},
		{"very-long-secret-key-123", "very****ey-123"},
		{"sk-live-abcdef123456", "sk-l****123456"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := maskToken(tt.input); got != tt.expected {
				t.Errorf("maskToken(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
