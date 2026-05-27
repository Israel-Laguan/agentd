package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"
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
	showKeys := configShowKeys(state)
	sources := make([]ConfigSource, 0, len(showKeys))
	for _, key := range showKeys {
		if name, field, ok := parseProviderConfigKey(key); ok {
			sources = append(sources, resolveProviderConfigSource(name, field, state))
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
			src.Source = "config.yaml"
			return src
		}
	}

	src.Source = "default"
	return src
}

func normalizeCompare(key, effective, raw string) bool {
	return effective == normalizeViperValue(raw) || effective == maskIfSensitive(key, raw)
}

func resolveProviderConfigSource(name, field string, state viperLoadState) ConfigSource {
	key := providerDisplayKey(name, field)
	src := ConfigSource{Key: key}

	fileProviders, err := gatewayProvidersFromViper(state.fileV)
	if err != nil {
		src.Source = "error"
		return src
	}
	fileP, ok := effectiveProviderByName(fileProviders, name)
	if !ok {
		src.Source = "default"
		return src
	}

	effectiveProviders, err := loadGatewayProviders(state.v, state.process, state.dotenv)
	if err != nil {
		src.Source = "error"
		return src
	}
	eff, ok := effectiveProviderByName(effectiveProviders, name)
	if !ok {
		src.Source = "default"
		return src
	}

	switch field {
	case "api_key":
		src.Value = maskIfSensitive("api_key", eff.APIKey)
		envKey := providerAPIKeyEnv(fileP)
		if envVal, ok := state.process[envKey]; ok && eff.APIKey == envVal {
			src.Source = envKey
		} else if envVal, ok := state.dotenv[envKey]; ok && eff.APIKey == envVal {
			src.Source = envKey + " (.env file)"
		} else if strings.TrimSpace(fileP.APIKey) != "" && eff.APIKey == fileP.APIKey {
			src.Source = "config.yaml"
		} else {
			src.Source = "default"
		}
		if fileKey := strings.TrimSpace(fileP.APIKey); fileKey != "" && eff.APIKey != fileKey {
			if envVal, envSrc := envOverrideSource(envKey, state.dotenv, state.process); envSrc != "" && eff.APIKey == envVal {
				src.OverriddenFrom = "config.yaml: " + maskIfSensitive("api_key", fileKey)
			}
		}
	case "base_url":
		src.Value = eff.BaseURL
		flatKey := "gateway." + strings.ReplaceAll(name, "-", "_") + ".base_url"
		if state.fileV.IsSet(flatKey) {
			if envVal, ok := state.process[envKeyForConfigKey(flatKey)]; ok && eff.BaseURL == envVal {
				src.Source = envKeyForConfigKey(flatKey)
			} else if envVal, ok := state.dotenv[envKeyForConfigKey(flatKey)]; ok && eff.BaseURL == envVal {
				src.Source = envKeyForConfigKey(flatKey) + " (.env file)"
			} else if eff.BaseURL == strings.TrimSpace(fileP.BaseURL) {
				src.Source = "config.yaml"
			} else {
				src.Source = "default"
			}
			if fileURL := strings.TrimSpace(fileP.BaseURL); fileURL != "" && eff.BaseURL != fileURL {
				if envVal, envSrc := envOverrideSource(envKeyForConfigKey(flatKey), state.dotenv, state.process); envSrc != "" && eff.BaseURL == envVal {
					src.OverriddenFrom = "config.yaml: " + fileURL
				}
			}
		} else if strings.TrimSpace(fileP.BaseURL) != "" && eff.BaseURL == fileP.BaseURL {
			src.Source = "config.yaml"
		} else {
			src.Source = "default"
		}
	default:
		src.Source = "default"
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
	return "config.yaml: " + maskIfSensitive(key, fileVal)
}
