package config

import (
	"strings"
	"testing"
)

func TestMonitoredGatewayFlatKeys_DiscoversCustomProvider(t *testing.T) {
	home := t.TempDir()
	configPath := home + "/config.yaml"
	writeOverrideTestConfig(t, configPath, `gateway:
  order: [poolside]
  poolside:
    api_key: sk-file
    base_url: https://file.example/v1
`)

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	keys := monitoredGatewayFlatKeys(fv)
	want := map[string]bool{
		"gateway.order":            true,
		"gateway.poolside.api_key": true,
		"gateway.poolside.base_url": true,
	}
	if len(keys) != len(want) {
		t.Fatalf("keys = %v, want %d entries", keys, len(want))
	}
	for _, key := range keys {
		if !want[key] {
			t.Errorf("unexpected key %q", key)
		}
	}
}

func TestDetectAllConfigOverrides_ProviderAPIKey(t *testing.T) {
	home := t.TempDir()
	configPath := home + "/config.yaml"
	writeOverrideTestConfig(t, configPath, `gateway:
  providers:
    - name: poolside
      adapter: openai
      api_key: sk-file
      base_url: https://inference.poolside.ai/v1
`)

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_POOLSIDE_API_KEY": "sk-env"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	o := overrides[0]
	if o.Key != "gateway.providers[name=poolside].api_key" {
		t.Errorf("Key = %q, want provider api_key display key", o.Key)
	}
	if o.EnvVar != "AGENTD_GATEWAY_POOLSIDE_API_KEY" {
		t.Errorf("EnvVar = %q, want AGENTD_GATEWAY_POOLSIDE_API_KEY", o.EnvVar)
	}
	if o.Source != "process env" {
		t.Errorf("Source = %q, want process env", o.Source)
	}
}

func TestLoad_LogsProviderListAPIKeyOverride(t *testing.T) {
	home := t.TempDir()
	configPath := home + "/config.yaml"
	writeOverrideTestConfig(t, configPath, `gateway:
  providers:
    - name: poolside
      adapter: openai
      api_key: sk-file
`)

	t.Setenv("AGENTD_GATEWAY_POOLSIDE_API_KEY", "sk-env")

	buf := captureInfoLogs(t)
	_, err := Load(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "gateway.providers[name=poolside].api_key") {
		t.Errorf("startup log missing provider override key: %s", output)
	}
	if !strings.Contains(output, "AGENTD_GATEWAY_POOLSIDE_API_KEY") {
		t.Errorf("startup log missing env var: %s", output)
	}
}

func TestConfigSources_ProviderAPIKeyOverrideNote(t *testing.T) {
	home := t.TempDir()
	configPath := home + "/config.yaml"
	writeOverrideTestConfig(t, configPath, `gateway:
  providers:
    - name: poolside
      adapter: openai
      api_key: sk-file
`)
	t.Setenv("AGENTD_GATEWAY_POOLSIDE_API_KEY", "sk-env")

	sources, _, err := ConfigSources(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("ConfigSources: %v", err)
	}
	line := findSourceLine(sources, "gateway.providers[name=poolside].api_key")
	if line == nil {
		t.Fatal("missing provider api_key line")
	}
	if line.Value != "****" {
		t.Errorf("Value = %q, want masked", line.Value)
	}
	if !strings.Contains(line.Source, "AGENTD_GATEWAY_POOLSIDE_API_KEY") {
		t.Errorf("Source = %q, want env var", line.Source)
	}
	if !strings.Contains(line.OverriddenFrom, "config.yaml") {
		t.Errorf("OverriddenFrom = %q, want config.yaml note", line.OverriddenFrom)
	}
}

func TestDetectAllConfigOverrides_ProviderAPIKey_GenericFallbackWhenExplicitMissing(t *testing.T) {
	home := t.TempDir()
	configPath := home + "/config.yaml"
	writeOverrideTestConfig(t, configPath, `gateway:
  providers:
    - name: poolside
      adapter: openai
      api_key: sk-file
      api_key_env: POOLSIDE_API_KEY
`)

	fv := newFileOnlyViper(home, configPath)
	if err := fv.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}

	process := map[string]string{"AGENTD_GATEWAY_POOLSIDE_API_KEY": "sk-env"}
	dotenv := map[string]string{}
	v := testViperAfterLoad(t, home, "", process, dotenv, true)

	overrides := detectAllConfigOverrides(fv, v, dotenv, process)
	if len(overrides) != 1 {
		t.Fatalf("want 1 override, got %d: %+v", len(overrides), overrides)
	}
	o := overrides[0]
	if o.EnvVar != "AGENTD_GATEWAY_POOLSIDE_API_KEY" {
		t.Errorf("EnvVar = %q, want AGENTD_GATEWAY_POOLSIDE_API_KEY", o.EnvVar)
	}
}

func TestConfigSources_ProviderAPIKeySource_GenericFallbackWhenExplicitMissing(t *testing.T) {
	home := t.TempDir()
	configPath := home + "/config.yaml"
	writeOverrideTestConfig(t, configPath, `gateway:
  providers:
    - name: poolside
      adapter: openai
      api_key: sk-file
      api_key_env: POOLSIDE_API_KEY
`)
	t.Setenv("AGENTD_GATEWAY_POOLSIDE_API_KEY", "sk-env")

	sources, _, err := ConfigSources(LoadOptions{HomeOverride: home})
	if err != nil {
		t.Fatalf("ConfigSources: %v", err)
	}
	line := findSourceLine(sources, "gateway.providers[name=poolside].api_key")
	if line == nil {
		t.Fatal("missing provider api_key line")
	}
	if line.Source != "AGENTD_GATEWAY_POOLSIDE_API_KEY" {
		t.Errorf("Source = %q, want AGENTD_GATEWAY_POOLSIDE_API_KEY", line.Source)
	}
}
