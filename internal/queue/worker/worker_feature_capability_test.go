package worker_test

import (
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

// ProviderSupportsChatTools enables workerTestGateway to satisfy the optional
// chatToolsChecker interface. Capability is resolved via a real router built from
// ProviderConfig, matching production behavior.
func (g *workerTestGateway) ProviderSupportsChatTools(provider string) bool {
	cfg, ok := providerConfigForFeatureTest(provider)
	if !ok {
		return false
	}
	r, err := gateway.NewRouterFromConfigs([]spec.ProviderConfig{cfg})
	if err != nil {
		return false
	}
	return r.ProviderSupportsChatTools(provider)
}

func providerConfigForFeatureTest(name string) (spec.ProviderConfig, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "synth-tools", "synth-openai":
		return spec.ProviderConfig{Name: name, Adapter: "openai", BaseURL: "https://example.com/v1", Model: "test-model", APIKey: "test-key"}, true
	case "synth-anthropic", "synth-worker", "synth-worker-fail":
		return spec.ProviderConfig{Name: name, Adapter: "anthropic", BaseURL: "https://example.com", Model: "test-model", APIKey: "test-key"}, true
	case "synth-no-tools", "synth-ollama", "synth-memory":
		return spec.ProviderConfig{Name: name, Adapter: "ollama", BaseURL: "http://localhost:11434", Model: "llama-test"}, true
	default:
		return spec.ProviderConfig{}, false
	}
}
