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

// ConnectorTopology returns a log-friendly single-line summary of the effective
// connector topology per role, e.g. "chat=direct(openai/gpt-4o-mini) worker=managed(litellm/poolside/laguna-m.1)".
func (c GatewayConfig) ConnectorTopology(configs []gateway.ProviderConfig) string {
	type roleInfo struct {
		provider string
		model    string
		managed  bool
	}
	infos := make(map[gateway.Role]roleInfo)

	// Build a lookup from provider name to config for model resolution.
	nameToCfg := make(map[string]gateway.ProviderConfig, len(configs))
	for _, cfg := range configs {
		nameToCfg[strings.ToLower(cfg.Name)] = cfg
	}

	defaultProvider := ""
	if len(configs) > 0 {
		defaultProvider = strings.ToLower(configs[0].Name)
	}

	resolve := func(provider, model string) (string, string, bool) {
		p := strings.ToLower(strings.TrimSpace(provider))
		if p == "" {
			p = defaultProvider
		}
		cfg, ok := nameToCfg[p]
		if !ok {
			return p, model, false
		}
		m := strings.TrimSpace(model)
		if m == "" {
			m = cfg.Model
		}
		_, managed := managedProviderNames[p]
		return p, m, managed
	}

	// Cache RoleRoutes() result to avoid repeated map allocations
	roleRoutes := c.RoleRoutes()
	for _, role := range []gateway.Role{gateway.RoleChat, gateway.RoleWorker, gateway.RoleMemory} {
		provider, model, managed := "", "", false
		if target, ok := roleRoutes[role]; ok {
			provider, model, managed = resolve(target.Provider, target.Model)
		} else {
			provider, model, managed = resolve(defaultProvider, "")
		}
		infos[role] = roleInfo{provider: provider, model: model, managed: managed}
	}

	order := []string{string(gateway.RoleChat), string(gateway.RoleWorker), string(gateway.RoleMemory)}
	parts := make([]string, 0, len(order))
	for _, role := range order {
		info := infos[gateway.Role(role)]
		label := "direct"
		if info.managed {
			label = "managed"
		}
		parts = append(parts, fmt.Sprintf("%s=%s(%s/%s)", role, label, info.provider, info.model))
	}
	return strings.Join(parts, " ")
}
