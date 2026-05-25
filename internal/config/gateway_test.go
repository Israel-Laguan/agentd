package config

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"agentd/internal/gateway"
)

func TestGatewayConfig_Defaults(t *testing.T) {
	cfg := GatewayConfig{
		Order:            []string{"openai", "ollama"},
		MaxTasksPerPhase: 7,
		Truncation: TruncationConfig{
			Strategy:       gateway.TruncationStrategyHeadTail,
			HeadRatio:      0.5,
			MaxInputChars:  12000,
			StashThreshold: 50000,
		},
		Truncator: TruncatorConfig{
			Policy:        gateway.TruncatorPolicyHeadTail,
			MaxInputChars: 12000,
		},
	}
	if len(cfg.Order) != 2 {
		t.Errorf("Order length = %v, want 2", len(cfg.Order))
	}
	if cfg.MaxTasksPerPhase != 7 {
		t.Errorf("MaxTasksPerPhase = %v, want 7", cfg.MaxTasksPerPhase)
	}
}

func TestGatewayConfig_ProviderConfigs(t *testing.T) {
	cfg := GatewayConfig{
		Order:     []string{"openai", "anthropic"},
		OpenAI:    gateway.ProviderConfig{Type: "openai", APIKey: "key1"},
		Anthropic: gateway.ProviderConfig{Type: "anthropic", APIKey: "key2"},
	}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}
	if len(configs) != 2 {
		t.Errorf("ProviderConfigs() length = %v, want 2", len(configs))
	}
	if configs[0].Type != "openai" {
		t.Errorf("first provider = %v, want openai", configs[0].Type)
	}
}

func TestGatewayConfig_ProviderConfigs_Gemini(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{"gemini"},
		Gemini: gateway.ProviderConfig{Type: "gemini", APIKey: "gem-key"},
	}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("ProviderConfigs() length = %v, want 1", len(configs))
	}
	if configs[0].Type != "gemini" {
		t.Errorf("provider type = %v, want gemini", configs[0].Type)
	}
}

func TestGatewayConfig_ProviderConfigs_EmptyOrder(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{},
		OpenAI: gateway.ProviderConfig{Type: "openai"},
	}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("ProviderConfigs() = %v, want empty", len(configs))
	}
}

func TestGatewayConfig_ProviderConfigs_CustomProvider(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"poolside"},
		Providers: []gateway.ProviderConfig{{
			Name:    "poolside",
			Type:    "openai",
			BaseURL: "https://inference.poolside.ai/v1",
			APIKey:  "poolside-key",
		}},
	}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("ProviderConfigs() length = %d, want 1", len(configs))
	}
	if configs[0].Name != "poolside" || configs[0].Type != "openai" {
		t.Fatalf("ProviderConfigs()[0] = %+v, want poolside/openai", configs[0])
	}
}

func TestGatewayConfig_ProviderConfigs_DuplicateNames(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"dup"},
		Providers: []gateway.ProviderConfig{
			{Name: "dup", Type: "openai"},
			{Name: "dup", Type: "anthropic"},
		},
	}
	if _, err := cfg.ProviderConfigs(); err == nil || !strings.Contains(err.Error(), "dup") {
		t.Fatalf("ProviderConfigs() error = %v, want duplicate error mentioning dup", err)
	}
}

func TestGatewayConfig_ProviderConfigs_EmptyProviderEntry(t *testing.T) {
	cfg := GatewayConfig{
		Order:     []string{"openai"},
		Providers: []gateway.ProviderConfig{{}},
	}
	_, err := cfg.ProviderConfigs()
	if err == nil {
		t.Fatal("ProviderConfigs() error = nil, want error for empty provider entry")
	}
	if !strings.Contains(err.Error(), "gateway.providers[0]") {
		t.Errorf("error = %v, want gateway.providers[0]", err)
	}
	if !strings.Contains(err.Error(), "name and adapter") {
		t.Errorf("error = %v, want mention of missing name and adapter", err)
	}
}

func TestGatewayConfig_ProviderConfigs_DuplicateOrderEntry(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{"openai", "openai"},
		OpenAI: gateway.ProviderConfig{Type: "openai", APIKey: "key1"},
	}
	if _, err := cfg.ProviderConfigs(); err == nil || !strings.Contains(err.Error(), "duplicate") || !strings.Contains(err.Error(), "gateway.order") {
		t.Fatalf("ProviderConfigs() error = %v, want duplicate error in gateway.order", err)
	}
}

func TestGatewayConfig_ProviderConfigs_UnknownOrderEntry(t *testing.T) {
	cfg := GatewayConfig{Order: []string{"nope"}}
	if _, err := cfg.ProviderConfigs(); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("ProviderConfigs() error = %v, want unknown name", err)
	}
}

func TestGatewayConfig_ProviderConfigs_BackwardCompat(t *testing.T) {
	cfg := GatewayConfig{
		Order:  []string{"openai"},
		OpenAI: gateway.ProviderConfig{Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "key1"},
	}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("ProviderConfigs() length = %d, want 1", len(configs))
	}
	if configs[0].Name != "openai" || configs[0].Type != "openai" {
		t.Fatalf("ProviderConfigs()[0] = %+v, want openai/openai", configs[0])
	}
}

func TestTruncationConfig_StrategyImpl(t *testing.T) {
	cfg := TruncationConfig{
		Strategy:  gateway.TruncationStrategyHeadTail,
		HeadRatio: 0.5,
	}
	strategy := cfg.StrategyImpl()
	if _, ok := strategy.(gateway.HeadTailStrategy); !ok {
		t.Errorf("StrategyImpl() = %T, want HeadTailStrategy", strategy)
	}
}

func TestTruncationConfig_StrategyImpl_MiddleOut(t *testing.T) {
	cfg := TruncationConfig{
		Strategy:  "middle_out",
		HeadRatio: 0.5,
	}
	strategy := cfg.StrategyImpl()
	if _, ok := strategy.(gateway.MiddleOutStrategy); !ok {
		t.Errorf("StrategyImpl() = %T, want MiddleOutStrategy", strategy)
	}
}

func TestGatewayConfig_RoleRoutes(t *testing.T) {
	cfg := GatewayConfig{
		RoleModels: RoleModelsConfig{
			Chat:   RoleModelConfig{Provider: "openai", Model: "gpt-4o"},
			Worker: RoleModelConfig{Provider: "anthropic"},
			Memory: RoleModelConfig{},
		},
	}
	routes := cfg.RoleRoutes()
	if len(routes) != 2 {
		t.Fatalf("RoleRoutes() length = %d, want 2", len(routes))
	}
	if routes[gateway.RoleChat].Provider != "openai" || routes[gateway.RoleChat].Model != "gpt-4o" {
		t.Errorf("chat route = %+v", routes[gateway.RoleChat])
	}
	if routes[gateway.RoleWorker].Provider != "anthropic" {
		t.Errorf("worker route = %+v", routes[gateway.RoleWorker])
	}
}

func TestGatewayConfig_RoleRoutes_Empty(t *testing.T) {
	if routes := (GatewayConfig{}).RoleRoutes(); routes != nil {
		t.Fatalf("RoleRoutes() = %v, want nil", routes)
	}
}

func TestDurationOrDefault(t *testing.T) {
	if durationOrDefault(5*time.Second, 10*time.Second) != 5*time.Second {
		t.Errorf("durationOrDefault(5s, 10s) = %v, want 5s", durationOrDefault(5*time.Second, 10*time.Second))
	}
	if durationOrDefault(0, 10*time.Second) != 10*time.Second {
		t.Errorf("durationOrDefault(0, 10s) = %v, want 10s", durationOrDefault(0, 10*time.Second))
	}
}

func TestGatewayConfig_ProviderConfigs_TwoOpenAIAdapters(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"openai", "poolside"},
		Providers: []gateway.ProviderConfig{
			{Name: "openai", Type: "openai", BaseURL: "https://api.openai.com/v1", APIKey: "key-oai"},
			{Name: "poolside", Type: "openai", BaseURL: "https://inference.poolside.ai/v1", APIKey: "key-ps"},
		},
	}
	configs, err := cfg.ProviderConfigs()
	if err != nil {
		t.Fatalf("ProviderConfigs() error = %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("ProviderConfigs() length = %d, want 2", len(configs))
	}
	if configs[0].Name != "openai" || configs[0].Type != "openai" || configs[0].BaseURL != "https://api.openai.com/v1" {
		t.Errorf("configs[0] = %+v", configs[0])
	}
	if configs[1].Name != "poolside" || configs[1].Type != "openai" || configs[1].BaseURL != "https://inference.poolside.ai/v1" {
		t.Errorf("configs[1] = %+v", configs[1])
	}
}

func TestGatewayConfig_ProviderConfigs_ImplicitNameCollision(t *testing.T) {
	// Two entries with adapter: openai but no explicit name both default to "openai" and collide.
	cfg := GatewayConfig{
		Order: []string{"openai"},
		Providers: []gateway.ProviderConfig{
			{Type: "openai", BaseURL: "https://api.openai.com/v1"},
			{Type: "openai", BaseURL: "https://inference.poolside.ai/v1"},
		},
	}
	if _, err := cfg.ProviderConfigs(); err == nil || !strings.Contains(err.Error(), "openai") {
		t.Fatalf("ProviderConfigs() error = %v, want duplicate error mentioning openai", err)
	}
}

func TestLoadMCPServers_NewKey(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  mcp_servers:
    - type: stdio
      name: mytool
      command: /usr/bin/mytool
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1", len(servers))
	}
	if servers[0].Name != "mytool" {
		t.Errorf("servers[0].Name = %q, want mytool", servers[0].Name)
	}
}

func TestLoadMCPServers_OldKeyFallback(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  capabilities:
    - type: stdio
      name: legacytool
      command: /usr/bin/legacytool
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1 (fallback to old key)", len(servers))
	}
	if servers[0].Name != "legacytool" {
		t.Errorf("servers[0].Name = %q, want legacytool", servers[0].Name)
	}
}

func TestLoadMCPServers_OldKeyIgnoredWhenNewPresent(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  mcp_servers:
    - type: stdio
      name: newtool
      command: /usr/bin/newtool
  capabilities:
    - type: stdio
      name: oldtool
      command: /usr/bin/oldtool
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1 (new key wins)", len(servers))
	}
	if servers[0].Name != "newtool" {
		t.Errorf("servers[0].Name = %q, want newtool (new key should win)", servers[0].Name)
	}
}

func TestLoadMCPServers_ExpandsAuthTokenEnv(t *testing.T) {
	t.Setenv("MCP_TOKEN", "secret-token")
	v := viper.New()
	v.SetConfigType("yaml")
	yaml := `
gateway:
  mcp_servers:
    - type: stdio
      name: securetool
      command: /usr/bin/securetool
      auth:
        type: bearer
        token: "${MCP_TOKEN}"
`
	if err := v.ReadConfig(strings.NewReader(yaml)); err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	servers, err := loadMCPServers(v)
	if err != nil {
		t.Fatalf("loadMCPServers() error = %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("loadMCPServers() len = %d, want 1", len(servers))
	}
	if servers[0].Auth.Token != "secret-token" {
		t.Fatalf("servers[0].Auth.Token = %q, want secret-token", servers[0].Auth.Token)
	}
}

func TestProviderConfigs_UnknownAdapter(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"custom"},
		Providers: []gateway.ProviderConfig{{
			Name: "custom",
			Type: "foo",
		}},
	}
	_, err := cfg.ProviderConfigs()
	if err == nil {
		t.Fatal("expected error for unknown adapter, got nil")
	}
	if !strings.Contains(err.Error(), "foo") {
		t.Errorf("error should mention the unknown adapter, got: %v", err)
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("error should mention the provider name, got: %v", err)
	}
}

func TestProviderConfigs_UnknownHealthValue(t *testing.T) {
	cfg := GatewayConfig{
		Order: []string{"custom"},
		Providers: []gateway.ProviderConfig{{
			Name:   "custom",
			Type:   "openai",
			APIKey: "sk-test",
			Health: "undefined_probe",
		}},
	}
	_, err := cfg.ProviderConfigs()
	if err == nil {
		t.Fatal("expected error for unknown health value, got nil")
	}
	if !strings.Contains(err.Error(), "undefined_probe") {
		t.Errorf("error should mention the unknown value, got: %v", err)
	}
	if !strings.Contains(err.Error(), "custom") {
		t.Errorf("error should mention the provider name, got: %v", err)
	}
}

func TestProviderConfigs_KnownHealthValues_Valid(t *testing.T) {
	tests := []struct {
		name string
		mode string
	}{
		{name: "api_key_trimmed_mixed_case", mode: "  Api_Key  "},
	}
	for _, mode := range allExplicitHealthModes {
		tests = append(tests, struct {
			name string
			mode string
		}{name: mode, mode: mode})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := GatewayConfig{
				Order: []string{"p"},
				Providers: []gateway.ProviderConfig{{
					Name:   "p",
					Type:   "openai",
					Health: tc.mode,
				}},
			}
			if _, err := cfg.ProviderConfigs(); err != nil {
				t.Errorf("ProviderConfigs() error = %v for known health mode %q", err, tc.mode)
			}
		})
	}
}
