package queue

import (
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

func testProviderConfigForName(name string) (spec.ProviderConfig, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "openai", "synth-tools", "synth-openai":
		return spec.ProviderConfig{Name: name, Adapter: "openai", BaseURL: "https://example.com/v1", Model: "test-model", APIKey: "test-key"}, true
	case "anthropic", "synth-anthropic":
		return spec.ProviderConfig{Name: name, Adapter: "anthropic", BaseURL: "https://example.com", Model: "test-model", APIKey: "test-key"}, true
	case "ollama", "synth-no-tools", "synth-ollama":
		return spec.ProviderConfig{Name: name, Adapter: "ollama", BaseURL: "http://localhost:11434", Model: "llama-test"}, true
	default:
		return spec.ProviderConfig{}, false
	}
}

func providerSupportsChatToolsViaRouter(provider string) bool {
	cfg, ok := testProviderConfigForName(provider)
	if !ok {
		return false
	}
	r, err := gateway.NewRouterFromConfigs([]spec.ProviderConfig{cfg})
	if err != nil {
		return false
	}
	return r.ProviderSupportsChatTools(provider)
}
