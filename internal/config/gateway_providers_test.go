package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"agentd/internal/gateway"
)

// newProvidersViper returns a viper instance with gateway.providers set to the
// given raw YAML value so that UnmarshalKey works correctly.
func newProvidersViper(t *testing.T, yaml string) *viper.Viper {
	t.Helper()
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(strings.NewReader("gateway:\n  providers:\n" + yaml)); err != nil {
		t.Fatalf("viper read config: %v", err)
	}
	return v
}

func TestLoadGatewayProviders_NameFallsBackToType(t *testing.T) {
	yaml := `    - adapter: openai
      health: api_key
`
	v := newProvidersViper(t, yaml)
	process := map[string]string{
		"AGENTD_GATEWAY_OPENAI_API_KEY": "sk-openai",
	}

	providers, err := loadGatewayProviders(v, process, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].Name != "openai" {
		t.Errorf("Name = %q, want %q", providers[0].Name, "openai")
	}
	if providers[0].APIKey != "sk-openai" {
		t.Errorf("APIKey = %q, want %q", providers[0].APIKey, "sk-openai")
	}
}

func TestLoadGatewayProviders_GenericEnvVar_HyphenatedName(t *testing.T) {
	yaml := `    - name: my-vendor
      adapter: openai
      health: api_key
`
	v := newProvidersViper(t, yaml)
	process := map[string]string{
		"AGENTD_GATEWAY_MY_VENDOR_API_KEY": "sk-hyphen",
	}

	providers, err := loadGatewayProviders(v, process, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].Name != "my-vendor" {
		t.Errorf("Name = %q, want %q", providers[0].Name, "my-vendor")
	}
	if providers[0].APIKey != "sk-hyphen" {
		t.Errorf("APIKey = %q, want %q (from AGENTD_GATEWAY_MY_VENDOR_API_KEY)", providers[0].APIKey, "sk-hyphen")
	}
}

func TestLoadGatewayProviders_GenericEnvVar(t *testing.T) {
	yaml := `    - name: poolside
      adapter: openai
      base_url: https://inference.poolside.ai/v1
      health: api_key
`
	v := newProvidersViper(t, yaml)
	process := map[string]string{
		"AGENTD_GATEWAY_POOLSIDE_API_KEY": "sk-poolside",
	}

	providers, err := loadGatewayProviders(v, process, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].APIKey != "sk-poolside" {
		t.Errorf("APIKey = %q, want %q (from AGENTD_GATEWAY_POOLSIDE_API_KEY)", providers[0].APIKey, "sk-poolside")
	}
}

func TestLoadGatewayProviders_GenericEnvVar_FromDotenv(t *testing.T) {
	yaml := `    - name: myvendor
      adapter: openai
`
	v := newProvidersViper(t, yaml)
	dotenv := map[string]string{
		"AGENTD_GATEWAY_MYVENDOR_API_KEY": "sk-dotenv",
	}

	providers, err := loadGatewayProviders(v, nil, dotenv)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].APIKey != "sk-dotenv" {
		t.Errorf("APIKey = %q, want %q (from dotenv AGENTD_GATEWAY_MYVENDOR_API_KEY)", providers[0].APIKey, "sk-dotenv")
	}
}

func TestLoadGatewayProviders_ExplicitAPIKeyEnv(t *testing.T) {
	yaml := `    - name: poolside
      adapter: openai
      api_key_env: MY_CUSTOM_KEY
`
	v := newProvidersViper(t, yaml)
	dotenv := map[string]string{
		"MY_CUSTOM_KEY": "sk-custom",
	}

	providers, err := loadGatewayProviders(v, nil, dotenv)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].APIKey != "sk-custom" {
		t.Errorf("APIKey = %q, want %q (from api_key_env MY_CUSTOM_KEY)", providers[0].APIKey, "sk-custom")
	}
}

func TestLoadGatewayProviders_ExplicitAPIKeyEnvTakesPrecedenceOverGeneric(t *testing.T) {
	yaml := `    - name: poolside
      adapter: openai
      api_key_env: MY_CUSTOM_KEY
`
	v := newProvidersViper(t, yaml)
	// Both env vars are set; the explicit api_key_env should win.
	process := map[string]string{
		"MY_CUSTOM_KEY":                   "sk-explicit",
		"AGENTD_GATEWAY_POOLSIDE_API_KEY": "sk-generic",
	}

	providers, err := loadGatewayProviders(v, process, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].APIKey != "sk-explicit" {
		t.Errorf("APIKey = %q, want sk-explicit (explicit api_key_env must take precedence over generic pattern)", providers[0].APIKey)
	}
}

func TestLoadGatewayProviders_GenericEnvVar_CaseInsensitiveName(t *testing.T) {
	// Provider names may use mixed case; the generic var must be upper-cased.
	yaml := `    - name: MyVendor
      adapter: openai
`
	v := newProvidersViper(t, yaml)
	process := map[string]string{
		"AGENTD_GATEWAY_MYVENDOR_API_KEY": "sk-upper",
	}

	providers, err := loadGatewayProviders(v, process, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}
	if providers[0].APIKey != "sk-upper" {
		t.Errorf("APIKey = %q, want sk-upper (name uppercased for generic env var)", providers[0].APIKey)
	}
}

func TestLoadGatewayProviders_EmptyKeyWarning(t *testing.T) {
	yaml := `    - name: poolside
      adapter: openai
      health: api_key
`
	v := newProvidersViper(t, yaml)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	providers, err := loadGatewayProviders(v, nil, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(providers))
	}

	got := buf.String()
	if !strings.Contains(got, "poolside") {
		t.Errorf("warning missing provider name %q; got: %s", "poolside", got)
	}
	if !strings.Contains(got, "AGENTD_GATEWAY_POOLSIDE_API_KEY") {
		t.Errorf("warning missing expected env var %q; got: %s", "AGENTD_GATEWAY_POOLSIDE_API_KEY", got)
	}
}

func TestLoadGatewayProviders_EmptyKeyWarning_UsesExplicitEnvHint(t *testing.T) {
	// When api_key_env is set but the env var is absent, the warning should
	// mention the explicitly configured env var, not the generic pattern.
	yaml := `    - name: poolside
      adapter: openai
      api_key_env: POOLSIDE_API_KEY
      health: api_key
`
	v := newProvidersViper(t, yaml)

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	_, err := loadGatewayProviders(v, nil, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, "POOLSIDE_API_KEY") {
		t.Errorf("warning should mention explicit api_key_env %q; got: %s", "POOLSIDE_API_KEY", got)
	}
}

func TestLoadGatewayProviders_NoWarningWhenKeyPresent(t *testing.T) {
	yaml := `    - name: poolside
      adapter: openai
      health: api_key
`
	v := newProvidersViper(t, yaml)
	process := map[string]string{
		"AGENTD_GATEWAY_POOLSIDE_API_KEY": "sk-poolside",
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	old := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })

	_, err := loadGatewayProviders(v, process, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}

	if buf.Len() != 0 {
		t.Errorf("unexpected warning emitted when key is present: %s", buf.String())
	}
}

func TestLoadGatewayProviders_EmptyEntry(t *testing.T) {
	v := newProvidersViper(t, "    - {}\n")
	providers, err := loadGatewayProviders(v, nil, nil)
	if err != nil {
		t.Fatalf("loadGatewayProviders() error = %v", err)
	}
	cfg := GatewayConfig{
		Order:     []string{"openai"},
		Providers: providers,
		OpenAI:    gateway.ProviderConfig{Type: "openai", APIKey: "sk-test"},
	}
	_, err = cfg.ProviderConfigs()
	if err == nil {
		t.Fatal("ProviderConfigs() error = nil, want error for empty gateway.providers entry")
	}
	if !strings.Contains(err.Error(), "gateway.providers[0]") {
		t.Errorf("error = %v, want gateway.providers[0]", err)
	}
	if !strings.Contains(err.Error(), "name and adapter") {
		t.Errorf("error = %v, want mention of missing name and adapter", err)
	}
}
