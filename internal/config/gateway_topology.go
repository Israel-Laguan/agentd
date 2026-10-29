package config

import (
	"fmt"
	"strings"

	"agentd/internal/gateway"
)

// managedProviderNames reports provider names that are recognized as managed
// proxies (LiteLLM, Portkey, OpenRouter). Classification is name-based so
// operators can control it via the gateway.providers[*].name field.
var managedProviderNames = map[string]struct{}{
	"litellm":    {},
	"portkey":    {},
	"openrouter": {},
}

// ConnectorTopology returns a log-friendly single-line summary of the gateway's
// default cascade, e.g. "cascade=managed(litellm/poolside/laguna-m.1)": the first
// provider in order, labelled managed or direct. Emitted at startup as slog
// "connector topology" with the "topology" value.
func (c GatewayConfig) ConnectorTopology(configs []gateway.ProviderConfig) string {
	// Build a lookup from provider name to config for model resolution.
	nameToCfg := make(map[string]gateway.ProviderConfig, len(configs))
	for _, cfg := range configs {
		nameToCfg[strings.ToLower(cfg.Name)] = cfg
	}

	provider := ""
	if len(configs) > 0 {
		provider = strings.ToLower(configs[0].Name)
	}
	model := ""
	if cfg, ok := nameToCfg[provider]; ok {
		model = cfg.Model
	}
	_, managed := managedProviderNames[provider]
	label := "direct"
	if managed {
		label = "managed"
	}
	return fmt.Sprintf("cascade=%s(%s/%s)", label, provider, model)
}
