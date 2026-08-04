package config

import (
	"testing"
	"time"

	"agentd/internal/gateway"
)

// topologyProvider returns a provider config usable as ConnectorTopology input.
func topologyProvider(name, model string) gateway.ProviderConfig {
	return gateway.ProviderConfig{
		Name:          name,
		Adapter:       "openai",
		BaseURL:       "http://127.0.0.1:4000/v1",
		APIKey:        "test",
		Model:         model,
		MaxInputChars: 0,
		Timeout:       5 * time.Minute,
	}
}

func TestGatewayConfig_ConnectorTopology(t *testing.T) {
	t.Parallel()
	litellm := topologyProvider("litellm", "poolside/laguna-m.1")
	openai := topologyProvider("openai", "gpt-4o-mini")
	ollama := topologyProvider("ollama", "llama3:8b")
	configs := []gateway.ProviderConfig{litellm, openai, ollama}

	// No role routes: all roles fall back to first provider in order (litellm).
	got := GatewayConfig{Order: []string{"litellm", "openai", "ollama"}}.ConnectorTopology(configs)
	want := "chat=managed(litellm/poolside/laguna-m.1) worker=managed(litellm/poolside/laguna-m.1) memory=managed(litellm/poolside/laguna-m.1)"
	if got != want {
		t.Errorf("no routes: got %q, want %q", got, want)
	}

	// With role routes: each role maps to its explicit provider/model.
	got = GatewayConfig{
		Order: []string{"litellm", "openai", "ollama"},
		RoleModels: RoleModelsConfig{
			Chat:   RoleModelConfig{Provider: "openai", Model: "gpt-4o"},
			Worker: RoleModelConfig{Provider: "litellm"},
			Memory: RoleModelConfig{Provider: "ollama"},
		},
	}.ConnectorTopology(configs)
	want = "chat=direct(openai/gpt-4o) worker=managed(litellm/poolside/laguna-m.1) memory=direct(ollama/llama3:8b)"
	if got != want {
		t.Errorf("with routes: got %q, want %q", got, want)
	}
}

func TestGatewayConfig_ConnectorTopology_ManagedProxiesAndEmpty(t *testing.T) {
	t.Parallel()
	portkey := topologyProvider("portkey", "")
	openrouter := topologyProvider("openrouter", "")

	// Portkey recognized as managed, openrouter too; un-routed memory falls back
	// to the first provider in order (portkey).
	got := GatewayConfig{
		Order: []string{"portkey", "openrouter"},
		RoleModels: RoleModelsConfig{
			Chat:   RoleModelConfig{Provider: "portkey"},
			Worker: RoleModelConfig{Provider: "openrouter"},
		},
	}.ConnectorTopology([]gateway.ProviderConfig{portkey, openrouter})
	want := "chat=managed(portkey/) worker=managed(openrouter/) memory=managed(portkey/)"
	if got != want {
		t.Errorf("managed proxies: got %q, want %q", got, want)
	}

	// Empty provider list: safe fallback with empty strings.
	if got := (GatewayConfig{}).ConnectorTopology(nil); got != "chat=direct(/) worker=direct(/) memory=direct(/)" {
		t.Errorf("empty configs: got %q", got)
	}
}
