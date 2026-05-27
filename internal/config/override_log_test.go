package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// writeOverrideTestConfig writes a minimal YAML config file for override tests.
func writeOverrideTestConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// captureInfoLogs swaps the default slog logger for a JSON handler writing to
// a buffer and restores the original on test cleanup.
func captureInfoLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	orig := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return &buf
}

// testViperAfterLoad builds the main viper after readConfig; when applyEnv is true,
// merged dotenv/process layers are applied via applyDotEnvToViper (simulating env win).
func testViperAfterLoad(t *testing.T, home, configFile string, process, dotenv map[string]string, applyEnv bool) *viper.Viper {
	t.Helper()
	cfg := baseConfig(home)
	v := newConfigViper(cfg, home, configFile)
	if err := readConfig(v, configFile); err != nil {
		t.Fatalf("readConfig: %v", err)
	}
	if applyEnv {
		merged := mergeDotEnv(dotenv, process)
		applyDotEnvToViper(v, merged, map[string]string{})
	}
	return v
}

// ---- detectOverrides unit tests ----

func TestDetectOverrides_ProcessEnvOverridesGatewayOrder(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_ORDER": "gemini"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)

	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	o := overrides[0]
	if o.Key != "gateway.order" {
		t.Errorf("Key = %q, want gateway.order", o.Key)
	}
	if o.EnvVar != "AGENTD_GATEWAY_ORDER" {
		t.Errorf("EnvVar = %q, want AGENTD_GATEWAY_ORDER", o.EnvVar)
	}
	if o.Source != "process env" {
		t.Errorf("Source = %q, want process env", o.Source)
	}
	if o.EffectiveValue != "gemini" {
		t.Errorf("EffectiveValue = %q, want gemini", o.EffectiveValue)
	}
	if o.FileValue != "horde" {
		t.Errorf("FileValue = %q, want horde", o.FileValue)
	}
}

func TestDetectOverrides_NoFalsePositiveWhenValuesAgree(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	// Env agrees with file.
	process := map[string]string{"AGENTD_GATEWAY_ORDER": "horde"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 0 {
		t.Errorf("want 0 overrides, got %d: %+v", len(overrides), overrides)
	}
}

func TestDetectOverrides_DotenvSourceAttribution(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	// Key is in dotenv only, not in process env.
	dotenv := map[string]string{"AGENTD_GATEWAY_ORDER": "gemini"}
	process := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	if overrides[0].Source != ".env file" {
		t.Errorf("Source = %q, want .env file", overrides[0].Source)
	}
}

func TestDetectOverrides_ProcessEnvWinsOverDotenv(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	// Both process env and dotenv set the key; process env wins.
	process := map[string]string{"AGENTD_GATEWAY_ORDER": "openai"}
	dotenv := map[string]string{"AGENTD_GATEWAY_ORDER": "gemini"}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	if overrides[0].Source != "process env" {
		t.Errorf("Source = %q, want process env", overrides[0].Source)
	}
	if overrides[0].EffectiveValue != "openai" {
		t.Errorf("EffectiveValue = %q, want openai", overrides[0].EffectiveValue)
	}
}

func TestDetectOverrides_APIKeyValueMasked(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  openai:\n    api_key: sk-file-key\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_OPENAI_API_KEY": "sk-env-key"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	o := overrides[0]
	if o.EffectiveValue != "****" {
		t.Errorf("EffectiveValue = %q, want ****", o.EffectiveValue)
	}
	if o.FileValue != "****" {
		t.Errorf("FileValue = %q, want ****", o.FileValue)
	}
}

func TestDetectOverrides_NoOverrideWhenKeyAbsentFromFile(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	// Config file does NOT set gateway.order.
	writeOverrideTestConfig(t, configPath, "api:\n  address: 127.0.0.1:8080\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_ORDER": "gemini"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 0 {
		t.Errorf("want 0 overrides (key not in file), got %d: %+v", len(overrides), overrides)
	}
}

func TestDetectOverrides_NoLogWhenExplicitConfigWins(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "explicit.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_ORDER": "gemini"}
	v := testViperAfterLoad(t, home, configPath, process, nil, false)

	overrides := detectAllConfigOverrides(fv, v, nil, process)
	if len(overrides) != 0 {
		t.Errorf("want 0 overrides when explicit config wins, got %d: %+v", len(overrides), overrides)
	}
}

func TestDetectOverrides_BaseURLOverride(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  openai:\n    base_url: https://my-proxy.example.com/v1\n")

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_OPENAI_BASE_URL": "https://api.openai.com/v1"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	if overrides[0].Key != "gateway.openai.base_url" {
		t.Errorf("Key = %q, want gateway.openai.base_url", overrides[0].Key)
	}
}

// ---- logConfigOverrides unit test ----

func TestLogConfigOverrides_EmitsStructuredInfoLog(t *testing.T) {
	buf := captureInfoLogs(t)

	logConfigOverrides([]configOverride{
		{
			Key:            "gateway.order",
			EnvVar:         "AGENTD_GATEWAY_ORDER",
			EffectiveValue: "gemini",
			FileValue:      "horde",
			Source:         "process env",
		},
	})

	output := buf.String()
	if !strings.Contains(output, "gateway.order") {
		t.Errorf("log output missing key: %s", output)
	}
	if !strings.Contains(output, "AGENTD_GATEWAY_ORDER") {
		t.Errorf("log output missing env_var: %s", output)
	}
	if !strings.Contains(output, "gemini") {
		t.Errorf("log output missing effective_value: %s", output)
	}
	if !strings.Contains(output, "horde") {
		t.Errorf("log output missing file_value: %s", output)
	}
}

func TestLogConfigOverrides_NoOutputForEmptySlice(t *testing.T) {
	buf := captureInfoLogs(t)
	logConfigOverrides(nil)
	if buf.Len() != 0 {
		t.Errorf("expected no log output, got: %s", buf.String())
	}
}

// ---- integration: Load() emits override log ----

func TestLoad_LogsOverrideAtStartup(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	t.Setenv("AGENTD_GATEWAY_ORDER", "gemini")

	buf := captureInfoLogs(t)

	_, err := Load(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gateway.order") {
		t.Errorf("startup log missing gateway.order override: %s", output)
	}
	if !strings.Contains(output, "AGENTD_GATEWAY_ORDER") {
		t.Errorf("startup log missing env var name: %s", output)
	}
}

func TestLoad_NoOverrideLogWhenNoConflict(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "api:\n  address: 127.0.0.1:9090\n")

	// No AGENTD_* env vars set.
	buf := captureInfoLogs(t)

	_, err := Load(LoadOptions{HomeOverride: home, ConfigFile: configPath})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	output := buf.String()
	if strings.Contains(output, "key overridden by env") {
		t.Errorf("unexpected override log: %s", output)
	}
}

func TestLoad_LogsOverrideFromDotenvFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("AGENTD_GATEWAY_ORDER=gemini\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	home := filepath.Join(dir, ".agentd")
	configPath := filepath.Join(home, "config.yaml")
	writeOverrideTestConfig(t, configPath, "gateway:\n  order: [horde]\n")

	t.Chdir(dir)
	if oldVal, had := os.LookupEnv("AGENTD_GATEWAY_ORDER"); had {
		_ = os.Unsetenv("AGENTD_GATEWAY_ORDER")
		t.Cleanup(func() { _ = os.Setenv("AGENTD_GATEWAY_ORDER", oldVal) })
	}
	buf := captureInfoLogs(t)

	_, err := Load(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gateway.order") {
		t.Errorf("startup log missing gateway.order override: %s", output)
	}
	if !strings.Contains(output, ".env file") {
		t.Errorf("startup log missing .env file source: %s", output)
	}
}

// ---- helper tests ----

func TestEnvKeyForConfigKey(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"gateway.order", "AGENTD_GATEWAY_ORDER"},
		{"gateway.openai.api_key", "AGENTD_GATEWAY_OPENAI_API_KEY"},
		{"gateway.openai.base_url", "AGENTD_GATEWAY_OPENAI_BASE_URL"},
	}
	for _, tt := range tests {
		if got := envKeyForConfigKey(tt.key); got != tt.want {
			t.Errorf("envKeyForConfigKey(%q) = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestMaskIfSensitive(t *testing.T) {
	tests := []struct {
		key  string
		val  string
		want string
	}{
		{"gateway.openai.api_key", "sk-real", "****"},
		{"gateway.openai.api_key", "", ""},
		{"gateway.openai.base_url", "https://example.com", "https://example.com"},
		{"gateway.order", "gemini", "gemini"},
	}
	for _, tt := range tests {
		if got := maskIfSensitive(tt.key, tt.val); got != tt.want {
			t.Errorf("maskIfSensitive(%q, %q) = %q, want %q", tt.key, tt.val, got, tt.want)
		}
	}
}

func TestNormalizeViperValue(t *testing.T) {
	tests := []struct {
		input any
		want  string
	}{
		{nil, ""},
		{"gemini", "gemini"},
		{[]interface{}{"horde"}, "horde"},
		{[]interface{}{"openai", "ollama"}, "openai,ollama"},
		{[]string{"gemini", "horde"}, "gemini,horde"},
		{42, "42"},
	}
	for _, tt := range tests {
		if got := normalizeViperValue(tt.input); got != tt.want {
			t.Errorf("normalizeViperValue(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
