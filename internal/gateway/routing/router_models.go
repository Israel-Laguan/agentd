package routing

import (
	"strings"

	"agentd/internal/gateway/route"
	"agentd/internal/gateway/spec"
)

func providerConfigName(cfg spec.ProviderConfig) string {
	name := strings.TrimSpace(cfg.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(cfg.Adapter)
}

func buildProviderModels(configs []spec.ProviderConfig) map[string][]string {
	out := make(map[string][]string)
	for _, cfg := range configs {
		name := providerConfigName(cfg)
		if name == "" {
			continue
		}
		out[name] = appendUniqueModel(out[name], cfg.Model)
	}
	return out
}

func appendUniqueModel(models []string, model string) []string {
	model = strings.TrimSpace(model)
	if model == "" {
		return models
	}
	for _, existing := range models {
		if strings.EqualFold(existing, model) {
			return models
		}
	}
	return append(models, model)
}

// mergeAgentModels records each routed agent's model as a known model of its
// provider, so the hints cover the models the routes can actually send to.
func mergeAgentModels(models map[string][]string, agents map[string]route.Agent) {
	for _, agent := range agents {
		provider := strings.TrimSpace(agent.Provider)
		if provider == "" {
			continue
		}
		models[provider] = appendUniqueModel(models[provider], agent.Model)
	}
}

func knownModelsForProvider(models map[string][]string, provider string) []string {
	if len(models) == 0 {
		return nil
	}
	provider = strings.TrimSpace(provider)
	for name, list := range models {
		if strings.EqualFold(name, provider) {
			out := make([]string, len(list))
			copy(out, list)
			return out
		}
	}
	return nil
}
