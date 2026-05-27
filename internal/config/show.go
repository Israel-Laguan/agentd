package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"

	"agentd/internal/gateway"
)

// ConfigSource describes one resolved config key, its effective value, and provenance.
type ConfigSource struct {
	Key            string
	Value          string
	Source         string
	OverriddenFrom string
}

func configShowKeys(state viperLoadState) []string {
	keys := append([]string{
		"api.address",
		"gateway.max_tasks_per_phase",
	}, monitoredGatewayFlatKeys(state.fileV)...)
	keys = append(keys, providerShowKeys(state.fileV)...)
	sort.Strings(keys)
	return keys
}

// ConfigSources resolves configuration and returns source annotations for config show.
func ConfigSources(opts LoadOptions) ([]ConfigSource, Config, error) {
	state, err := prepareViperLoad(opts)
	if err != nil {
		return nil, Config{}, err
	}

	explicitV := explicitFileViper(opts.ConfigFile)
	providerCache := buildProviderResolveCache(state)
	showKeys := configShowKeys(state)
	sources := make([]ConfigSource, 0, len(showKeys))
	for _, key := range showKeys {
		if name, field, ok := parseProviderConfigKey(key); ok {
			sources = append(sources, resolveProviderConfigSource(name, field, state, providerCache))
			continue
		}
		sources = append(sources, resolveConfigSource(key, state, explicitV))
	}

	state.v.Set("home", state.cfg.HomeDir)
	cfg, err := hydrateConfig(state.cfg, state.v, state.process, state.dotenv)
	if err != nil {
		return nil, Config{}, err
	}
	return sources, cfg, nil
}

// FormatConfigSourceLine renders one ConfigSource for CLI output.
func FormatConfigSourceLine(s ConfigSource) string {
	if s.OverriddenFrom != "" {
		return fmt.Sprintf("%s=%s (source: %s; overrides %s)", s.Key, s.Value, s.Source, s.OverriddenFrom)
	}
	return fmt.Sprintf("%s=%s (source: %s)", s.Key, s.Value, s.Source)
}

func explicitFileViper(configFile string) *viper.Viper {
	if configFile == "" {
		return nil
	}
	fv := viper.New()
	fv.SetConfigFile(configFile)
	_ = fv.ReadInConfig()
	return fv
}

func resolveConfigSource(key string, state viperLoadState, explicitV *viper.Viper) ConfigSource {
	effective := maskIfSensitive(key, normalizeViperValue(state.v.Get(key)))
	envKey := envKeyForConfigKey(key)

	src := ConfigSource{
		Key:   key,
		Value: effective,
	}

	if state.opts.ConfigFile != "" && explicitV != nil && explicitV.IsSet(key) {
		explicitVal := normalizeViperValue(explicitV.Get(key))
		if effective == maskIfSensitive(key, explicitVal) || effective == explicitVal {
			src.Source = "explicit config"
			return src
		}
	}

	if envVal, ok := state.process[envKey]; ok {
		if effective == maskIfSensitive(key, envVal) || normalizeCompare(key, effective, envVal) {
			src.Source = envKey
			src.OverriddenFrom = overrideNote(state, key)
			return src
		}
	}

	if envVal, ok := state.dotenv[envKey]; ok {
		if effective == maskIfSensitive(key, envVal) || normalizeCompare(key, effective, envVal) {
			src.Source = envKey + " (.env file)"
			src.OverriddenFrom = overrideNote(state, key)
			return src
		}
	}

	if state.fileV.IsSet(key) {
		fileVal := normalizeViperValue(state.fileV.Get(key))
		if effective == maskIfSensitive(key, fileVal) || effective == fileVal {
			src.Source = fileConfigSourceLabel(state)
			return src
		}
	}

	src.Source = "default"
	return src
}

func normalizeCompare(key, effective, raw string) bool {
	return effective == normalizeViperValue(raw) || effective == maskIfSensitive(key, raw)
}

type providerResolveCache struct {
	fileProviders      []gateway.ProviderConfig
	effectiveProviders []gateway.ProviderConfig
	fileErr            error
	effErr             error
}

func buildProviderResolveCache(state viperLoadState) providerResolveCache {
	fileProviders, fileErr := gatewayProvidersFromViper(state.fileV)
	effectiveProviders, effErr := loadGatewayProviders(state.v, state.process, state.dotenv)
	return providerResolveCache{
		fileProviders:      fileProviders,
		effectiveProviders: effectiveProviders,
		fileErr:            fileErr,
		effErr:             effErr,
	}
}

func fileConfigSourceLabel(state viperLoadState) string {
	if state.opts.ConfigFile != "" {
		return "explicit config"
	}
	return "config.yaml"
}

func fileConfigOverridePrefix(state viperLoadState) string {
	return fileConfigSourceLabel(state) + ": "
}

func resolveProviderConfigSource(name, field string, state viperLoadState, cache providerResolveCache) ConfigSource {
	key := providerDisplayKey(name, field)
	src := ConfigSource{Key: key}

	if cache.fileErr != nil {
		src.Source = "error"
		return src
	}
	fileP, ok := effectiveProviderByName(cache.fileProviders, name)
	if !ok {
		src.Source = "default"
		return src
	}

	if cache.effErr != nil {
		src.Source = "error"
		return src
	}
	eff, ok := effectiveProviderByName(cache.effectiveProviders, name)
	if !ok {
		src.Source = "default"
		return src
	}

	switch field {
	case "api_key":
		return resolveProviderAPIKeyConfigSource(src, fileP, eff, state)
	case "base_url":
		return resolveProviderBaseURLConfigSource(src, name, fileP, eff, state)
	default:
		src.Source = "default"
	}
	return src
}

func resolveProviderAPIKeyConfigSource(
	src ConfigSource,
	fileP, eff gateway.ProviderConfig,
	state viperLoadState,
) ConfigSource {
	src.Value = maskIfSensitive("api_key", eff.APIKey)
	envKey := providerAPIKeyEnv(fileP)
	if envVal, ok := state.process[envKey]; ok && eff.APIKey == envVal {
		src.Source = envKey
	} else if envVal, ok := state.dotenv[envKey]; ok && eff.APIKey == envVal {
		src.Source = envKey + " (.env file)"
	} else if strings.TrimSpace(fileP.APIKey) != "" && eff.APIKey == fileP.APIKey {
		src.Source = fileConfigSourceLabel(state)
	} else {
		src.Source = "default"
	}
	if fileKey := strings.TrimSpace(fileP.APIKey); fileKey != "" && eff.APIKey != fileKey {
		if envVal, envSrc := envOverrideSource(envKey, state.dotenv, state.process); envSrc != "" && eff.APIKey == envVal {
			src.OverriddenFrom = fileConfigOverridePrefix(state) + maskIfSensitive("api_key", fileKey)
		}
	}
	return src
}

func resolveProviderBaseURLConfigSource(
	src ConfigSource,
	name string,
	fileP, eff gateway.ProviderConfig,
	state viperLoadState,
) ConfigSource {
	src.Value = eff.BaseURL
	flatKey := "gateway." + strings.ReplaceAll(name, "-", "_") + ".base_url"
	envKey := envKeyForConfigKey(flatKey)
	if envVal, ok := state.process[envKey]; ok && eff.BaseURL == envVal {
		src.Source = envKey
	} else if envVal, ok := state.dotenv[envKey]; ok && eff.BaseURL == envVal {
		src.Source = envKey + " (.env file)"
	} else if strings.TrimSpace(fileP.BaseURL) != "" && eff.BaseURL == strings.TrimSpace(fileP.BaseURL) {
		src.Source = fileConfigSourceLabel(state)
	} else {
		src.Source = "default"
	}
	if fileURL := strings.TrimSpace(fileP.BaseURL); fileURL != "" && eff.BaseURL != fileURL {
		if envVal, envSrc := envOverrideSource(envKey, state.dotenv, state.process); envSrc != "" && eff.BaseURL == envVal {
			src.OverriddenFrom = fileConfigOverridePrefix(state) + fileURL
		}
	}
	return src
}

func overrideNote(state viperLoadState, key string) string {
	if !state.fileV.IsSet(key) {
		return ""
	}
	envKey := envKeyForConfigKey(key)
	envVal, _ := envOverrideSource(envKey, state.dotenv, state.process)
	if envVal == "" {
		return ""
	}
	fileVal := normalizeViperValue(state.fileV.Get(key))
	if fileVal == envVal {
		return ""
	}
	effective := normalizeViperValue(state.v.Get(key))
	if effective != envVal {
		return ""
	}
	return fileConfigOverridePrefix(state) + maskIfSensitive(key, fileVal)
}
