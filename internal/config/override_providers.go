package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"

	"agentd/internal/gateway"
)

const providerConfigKeyPrefix = "gateway.providers[name="

// providerDisplayKey is the stable label for gateway.providers entries in logs and config show.
func providerDisplayKey(name, field string) string {
	return providerConfigKeyPrefix + name + "]." + field
}

func parseProviderConfigKey(key string) (name, field string, ok bool) {
	if !strings.HasPrefix(key, providerConfigKeyPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, providerConfigKeyPrefix)
	name, field, ok = strings.Cut(rest, "].")
	return name, field, ok
}

func providerName(p gateway.ProviderConfig) string {
	if name := strings.TrimSpace(p.Name); name != "" {
		return name
	}
	return strings.TrimSpace(p.Adapter)
}

func providerAPIKeyEnv(p gateway.ProviderConfig) string {
	if keys := providerAPIKeyEnvCandidates(p); len(keys) > 0 {
		return keys[0]
	}
	return ""
}

func providerAPIKeyEnvCandidates(p gateway.ProviderConfig) []string {
	keys := make([]string, 0, 2)
	if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
		keys = append(keys, env)
	}
	if name := providerName(p); name != "" {
		generic := genericGatewayAPIKeyEnv(name)
		if generic != "" && (len(keys) == 0 || keys[0] != generic) {
			keys = append(keys, generic)
		}
	}
	return keys
}

func providerAPIKeyEnvOverride(
	fileP gateway.ProviderConfig,
	effectiveValue string,
	dotenv, process map[string]string,
) (envKey, envVal, source string, ok bool) {
	for _, key := range providerAPIKeyEnvCandidates(fileP) {
		val, src := envOverrideSource(key, dotenv, process)
		if src != "" && (effectiveValue == "" || val == effectiveValue) {
			return key, val, src, true
		}
	}
	return "", "", "", false
}

func gatewayProvidersFromViper(v *viper.Viper) ([]gateway.ProviderConfig, error) {
	if v == nil || !v.IsSet("gateway.providers") {
		return nil, nil
	}
	var providers []gateway.ProviderConfig
	if err := v.UnmarshalKey("gateway.providers", &providers); err != nil {
		return nil, fmt.Errorf("unmarshal gateway.providers: %w", err)
	}
	return providers, nil
}

func effectiveProviderByName(providers []gateway.ProviderConfig, name string) (gateway.ProviderConfig, bool) {
	for _, p := range providers {
		if providerName(p) == name {
			return p, true
		}
	}
	return gateway.ProviderConfig{}, false
}

func providerShowKeys(fv *viper.Viper) []string {
	providers, err := gatewayProvidersFromViper(fv)
	if err != nil || len(providers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(providers)*2)
	for _, p := range providers {
		name := providerName(p)
		if name == "" {
			continue
		}
		if strings.TrimSpace(p.APIKey) != "" || providerAPIKeyEnv(p) != "" {
			keys = append(keys, providerDisplayKey(name, "api_key"))
		}
		if strings.TrimSpace(p.BaseURL) != "" {
			keys = append(keys, providerDisplayKey(name, "base_url"))
		}
	}
	return keys
}

// detectProviderEntryOverrides reports env/dotenv wins over inline gateway.providers api_key values.
func detectProviderEntryOverrides(fv, v *viper.Viper, dotenv, process map[string]string) []configOverride {
	fileProviders, err := gatewayProvidersFromViper(fv)
	if err != nil || len(fileProviders) == 0 {
		return nil
	}
	effectiveProviders, err := loadGatewayProvidersNoWarn(v, process, dotenv)
	if err != nil {
		return nil
	}

	var overrides []configOverride
	for _, fileP := range fileProviders {
		name := providerName(fileP)
		if name == "" {
			continue
		}
		if o, ok := detectProviderAPIKeyOverride(fileP, name, effectiveProviders, dotenv, process); ok {
			overrides = append(overrides, o)
		}
	}
	return overrides
}

func detectProviderAPIKeyOverride(
	fileP gateway.ProviderConfig,
	name string,
	effectiveProviders []gateway.ProviderConfig,
	dotenv, process map[string]string,
) (configOverride, bool) {
	fileKey := strings.TrimSpace(fileP.APIKey)
	if fileKey == "" {
		return configOverride{}, false
	}
	eff, ok := effectiveProviderByName(effectiveProviders, name)
	if !ok {
		return configOverride{}, false
	}
	envKey, envVal, source, found := providerAPIKeyEnvOverride(fileP, eff.APIKey, dotenv, process)
	if !found || envVal == fileKey {
		return configOverride{}, false
	}
	return configOverride{
		Key:            providerDisplayKey(name, "api_key"),
		EnvVar:         envKey,
		EffectiveValue: maskIfSensitive("api_key", envVal),
		FileValue:      maskIfSensitive("api_key", fileKey),
		Source:         source,
	}, true
}
