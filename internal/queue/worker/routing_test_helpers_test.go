package worker

import (
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
)

// testProviderConfig returns a minimal ProviderConfig for routing/capability tests.
// Unknown names return ok=false.
func testProviderConfig(name string) (spec.ProviderConfig, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "openai", "synth-tools", "synth-openai":
		return toolCapableConfig(coalesceName(name, "synth-openai"), "openai"), true
	case "anthropic", "synth-anthropic", "synth-worker":
		return toolCapableConfig(coalesceName(name, "synth-anthropic"), "anthropic"), true
	case "ollama", "synth-ollama", "synth-no-tools", "synth-memory":
		return noToolConfig(coalesceName(name, "synth-ollama")), true
	case "llamacpp", "synth-llamacpp":
		return spec.ProviderConfig{Name: coalesceName(name, "synth-llamacpp"), Adapter: "llamacpp", BaseURL: "http://localhost:8080", Model: "local"}, true
	case "horde", "synth-horde":
		return spec.ProviderConfig{Name: coalesceName(name, "synth-horde"), Adapter: "horde", BaseURL: "https://stablehorde.net/api/v2", Model: "aphrodite"}, true
	case "poolside", "synth-openai-compat", "synth-openai-a", "synth-openai-b", "synth-chat", "synth-worker-fail":
		return toolCapableConfig(name, "openai"), true
	default:
		return spec.ProviderConfig{}, false
	}
}

func coalesceName(actual, defaultName string) string {
	if strings.TrimSpace(actual) != "" {
		return actual
	}
	return defaultName
}

// standardRoutingTestConfigs returns providers commonly used in worker_routing_test.go,
// including model-router tier targets (openai, anthropic, ollama).
func standardRoutingTestConfigs() []spec.ProviderConfig {
	return []spec.ProviderConfig{
		toolCapableConfig("openai", "openai"),
		toolCapableConfig("anthropic", "anthropic"),
		noToolConfig("ollama"),
	}
}

// routingConfigsForProfile builds router configs for worker routing decision tests.
func routingConfigsForProfile(provider string) []spec.ProviderConfig {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return []spec.ProviderConfig{toolCapableConfig("synth-openai", "openai")}
	}
	cfg, ok := testProviderConfig(provider)
	if !ok {
		return nil
	}
	cfgs := standardRoutingTestConfigs()
	cfgs = append(cfgs, cfg)
	return cfgs
}

func buildTestCapabilityRouter(cfgs ...spec.ProviderConfig) (*gateway.Router, error) {
	if len(cfgs) == 0 {
		return nil, nil
	}
	return gateway.NewRouterFromConfigs(cfgs)
}
