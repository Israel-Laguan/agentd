package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigSources_DotenvOverrideNote(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, ".agentd")
	writeOverrideTestConfig(t, filepath.Join(home, "config.yaml"), "gateway:\n  order: [horde]\n")
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("AGENTD_GATEWAY_ORDER=gemini\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	t.Chdir(dir)

	sources, _, err := ConfigSources(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("ConfigSources: %v", err)
	}
	line := findSourceLine(sources, "gateway.order")
	if line == nil {
		t.Fatal("missing gateway.order")
	}
	if line.Value != "gemini" {
		t.Errorf("Value = %q, want gemini", line.Value)
	}
	if !strings.Contains(line.Source, "AGENTD_GATEWAY_ORDER") {
		t.Errorf("Source = %q, want env var name", line.Source)
	}
	if !strings.Contains(line.OverriddenFrom, "config.yaml") {
		t.Errorf("OverriddenFrom = %q, want config.yaml note", line.OverriddenFrom)
	}
}

func TestConfigSources_NoOverrideNoteWhenValuesAgree(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	t.Setenv("AGENTD_GATEWAY_ORDER", "horde")

	sources, _, err := ConfigSources(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("ConfigSources: %v", err)
	}
	line := findSourceLine(sources, "gateway.order")
	if line == nil {
		t.Fatal("missing gateway.order")
	}
	if line.OverriddenFrom != "" {
		t.Errorf("OverriddenFrom = %q, want empty", line.OverriddenFrom)
	}
}

func TestFormatConfigSourceLine(t *testing.T) {
	got := FormatConfigSourceLine(ConfigSource{
		Key:            "gateway.order",
		Value:          "gemini",
		Source:         "AGENTD_GATEWAY_ORDER",
		OverriddenFrom: "config.yaml: horde",
	})
	want := "gateway.order=gemini (source: AGENTD_GATEWAY_ORDER; overrides config.yaml: horde)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func findSourceLine(sources []ConfigSource, key string) *ConfigSource {
	for i := range sources {
		if sources[i].Key == key {
			return &sources[i]
		}
	}
	return nil
}
