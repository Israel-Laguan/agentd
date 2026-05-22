package config

import (
	"testing"
	"time"

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
	configs := cfg.ProviderConfigs()
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
	configs := cfg.ProviderConfigs()
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
	configs := cfg.ProviderConfigs()
	if len(configs) != 0 {
		t.Errorf("ProviderConfigs() = %v, want empty", len(configs))
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
