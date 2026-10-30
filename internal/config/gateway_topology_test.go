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

	// The cascade starts at the first provider in order.
	got := GatewayConfig{Order: []string{"litellm", "openai", "ollama"}}.ConnectorTopology(configs)
	want := "cascade=managed(litellm/poolside/laguna-m.1)"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestGatewayConfig_ConnectorTopology_ManagedProxiesAndEmpty(t *testing.T) {
	t.Parallel()
	portkey := topologyProvider("portkey", "")

	if got := (GatewayConfig{Order: []string{"portkey"}}).ConnectorTopology([]gateway.ProviderConfig{portkey}); got != "cascade=managed(portkey/)" {
		t.Errorf("managed proxy: got %q", got)
	}

	// Empty provider list: safe fallback with empty strings.
	if got := (GatewayConfig{}).ConnectorTopology(nil); got != "cascade=direct(/)" {
		t.Errorf("empty configs: got %q", got)
	}
}
