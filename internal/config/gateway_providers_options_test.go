package config

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// newHordeViper returns a viper with the given gateway.horde.poll_interval value.
func newHordeViper(t *testing.T, pollInterval string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("gateway:\n  horde:\n    poll_interval: " + pollInterval + "\n")); err != nil {
		t.Fatalf("viper read config: %v", err)
	}
	return v
}

func TestLoadGatewayProviderConfigs_HordePollIntervalLandsInOptions(t *testing.T) {
	// Backward-compat: the legacy gateway.horde.poll_interval viper key must
	// populate Options["poll_interval"] as a time.Duration.
	v := newHordeViper(t, "5s")
	_, _, _, _, horde, _ := loadGatewayProviderConfigs(v, nil, nil)

	pi, ok := horde.Options["poll_interval"]
	if !ok {
		t.Fatal(`Options["poll_interval"] not set`)
	}
	d, ok := pi.(time.Duration)
	if !ok {
		t.Fatalf(`Options["poll_interval"] type = %T, want time.Duration`, pi)
	}
	if d != 5*time.Second {
		t.Fatalf("poll_interval = %v, want 5s", d)
	}
}

func TestLoadGatewayProviderConfigs_HordeOptionsPollIntervalPreferred(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `gateway:
  horde:
    poll_interval: 5s
    options:
      poll_interval: 6s
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("viper read config: %v", err)
	}
	_, _, _, _, horde, _ := loadGatewayProviderConfigs(v, nil, nil)

	pi, ok := horde.Options["poll_interval"]
	if !ok {
		t.Fatal(`Options["poll_interval"] not set`)
	}
	d, ok := pi.(time.Duration)
	if !ok {
		t.Fatalf(`Options["poll_interval"] type = %T, want time.Duration`, pi)
	}
	if d != 6*time.Second {
		t.Fatalf("poll_interval = %v, want 6s (options key preferred)", d)
	}
}

func TestLoadGatewayProviderConfigs_HordeOptionsPollIntervalOnly(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `gateway:
  horde:
    options:
      poll_interval: 7s
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("viper read config: %v", err)
	}
	_, _, _, _, horde, _ := loadGatewayProviderConfigs(v, nil, nil)

	pi, ok := horde.Options["poll_interval"]
	if !ok {
		t.Fatal(`Options["poll_interval"] not set`)
	}
	d, ok := pi.(time.Duration)
	if !ok {
		t.Fatalf(`Options["poll_interval"] type = %T, want time.Duration`, pi)
	}
	if d != 7*time.Second {
		t.Fatalf("poll_interval = %v, want 7s", d)
	}
}

func TestLoadGatewayProviderConfigs_HordePollIntervalDefault(t *testing.T) {
	// When gateway.horde.poll_interval is not set the default (4s) is used.
	v := viper.New()
	_, _, _, _, horde, _ := loadGatewayProviderConfigs(v, nil, nil)

	pi, ok := horde.Options["poll_interval"]
	if !ok {
		t.Fatal(`Options["poll_interval"] not set`)
	}
	d, ok := pi.(time.Duration)
	if !ok {
		t.Fatalf(`Options["poll_interval"] type = %T, want time.Duration`, pi)
	}
	if d != 4*time.Second {
		t.Fatalf("poll_interval = %v, want 4s (default)", d)
	}
}

func TestLoadGatewayProviders_OptionsMapPassedThrough(t *testing.T) {
	// Dynamic gateway.providers entries with an options block must have their
	// Options map populated by UnmarshalKey so adapters can read them.
	yaml := `    - name: horde
      adapter: horde
      options:
        poll_interval: "5s"
`
	v := newProvidersViper(t, yaml)
	providers, err := loadGatewayProviders(v, nil, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].Options == nil {
		t.Fatal("Options is nil, want non-nil")
	}
	pi, ok := providers[0].Options["poll_interval"]
	if !ok {
		t.Fatal(`Options["poll_interval"] not set`)
	}
	if pi != "5s" {
		t.Fatalf(`Options["poll_interval"] = %v, want "5s"`, pi)
	}
}

func TestLoadGatewayProviderConfigs_LlamaCppFlatSlotCapabilitiesAndOptions(t *testing.T) {
	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `gateway:
  llamacpp:
    base_url: http://127.0.0.1:8080
    model: tool-model
    capabilities:
      chat_tools: true
    options:
      probe_tools: true
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("viper read config: %v", err)
	}
	_, _, _, llamaCpp, _, _ := loadGatewayProviderConfigs(v, nil, nil)

	if llamaCpp.BaseURL != "http://127.0.0.1:8080" {
		t.Errorf("BaseURL = %q", llamaCpp.BaseURL)
	}
	if llamaCpp.Model != "tool-model" {
		t.Errorf("Model = %q", llamaCpp.Model)
	}
	if llamaCpp.Capabilities.ChatTools == nil || *llamaCpp.Capabilities.ChatTools != true {
		t.Errorf("Capabilities.ChatTools = %v, want true", llamaCpp.Capabilities.ChatTools)
	}
	if llamaCpp.Options == nil {
		t.Fatal("Options is nil")
	}
	probe, ok := llamaCpp.Options["probe_tools"]
	if !ok {
		t.Fatal(`Options["probe_tools"] not set`)
	}
	if probe != true {
		t.Fatalf(`Options["probe_tools"] = %v, want true`, probe)
	}
}

func TestLoadGatewayProviderConfigs_LlamaCppFlatSlotDefaults(t *testing.T) {
	v := viper.New()
	_, _, _, llamaCpp, _, _ := loadGatewayProviderConfigs(v, nil, nil)

	if llamaCpp.Capabilities.ChatTools != nil {
		t.Errorf("default Capabilities.ChatTools = %v, want nil", llamaCpp.Capabilities.ChatTools)
	}
	if llamaCpp.Options != nil {
		t.Errorf("default Options = %v, want nil", llamaCpp.Options)
	}
}
