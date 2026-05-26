package routing

import (
	"strings"

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

func mergeRoleRouteModels(models map[string][]string, routes map[spec.Role]spec.RoleTarget) {
	for _, target := range routes {
		provider := strings.TrimSpace(target.Provider)
		if provider == "" {
			continue
		}
		models[provider] = appendUniqueModel(models[provider], target.Model)
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
