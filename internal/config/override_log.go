package config

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/spf13/viper"
)

// monitoredConfigKeys lists the config keys checked for env-override logging
// at startup. Covers the minimum set required: gateway.order, provider
// base_url, and provider api_key fields.
var monitoredConfigKeys = []string{
	"gateway.order",
	"gateway.openai.api_key",
	"gateway.openai.base_url",
	"gateway.anthropic.api_key",
	"gateway.anthropic.base_url",
	"gateway.ollama.base_url",
	"gateway.llamacpp.base_url",
	"gateway.horde.api_key",
	"gateway.horde.base_url",
	"gateway.gemini.api_key",
	"gateway.gemini.base_url",
}

type configOverride struct {
	Key            string
	EnvVar         string
	EffectiveValue string
	FileValue      string
	Source         string // "process env" or ".env file"
}

// envKeyForConfigKey converts a viper config key to its AGENTD_* env var name.
// e.g. "gateway.order" → "AGENTD_GATEWAY_ORDER"
func envKeyForConfigKey(key string) string {
	return envPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
}

// maskIfSensitive replaces non-empty values for sensitive keys (api_key)
// with "****" so secrets are not written to logs.
func maskIfSensitive(key, val string) string {
	if strings.HasSuffix(key, "api_key") && val != "" {
		return "****"
	}
	return val
}

// newFileOnlyViper creates a viper that reads only the config file — no
// AutomaticEnv, no v.Set overrides — to snapshot the values the operator
// wrote to config.yaml (or the explicit config file).
func newFileOnlyViper(homeDir, configFile string) *viper.Viper {
	fv := viper.New()
	if configFile != "" {
		fv.SetConfigFile(configFile)
	} else {
		fv.SetConfigName("config")
		fv.SetConfigType("yaml")
		fv.AddConfigPath(homeDir)
	}
	return fv
}

// normalizeViperValue converts a value returned by viper.Get to a canonical
// string for comparison with a raw env-var string. Slices are joined with
// commas so that "[horde]" (file) matches "horde" (env).
func normalizeViperValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case []interface{}:
		parts := make([]string, len(val))
		for i, p := range val {
			parts[i] = fmt.Sprintf("%v", p)
		}
		return strings.Join(parts, ",")
	case []string:
		return strings.Join(val, ",")
	default:
		return fmt.Sprintf("%v", val)
	}
}

// envOverrideSource returns the raw env value and its source label for the
// given env key. Process env takes precedence over dotenv.
func envOverrideSource(envKey string, dotenv, process map[string]string) (string, string) {
	if val, ok := process[envKey]; ok {
		return val, "process env"
	}
	if val, ok := dotenv[envKey]; ok {
		return val, ".env file"
	}
	return "", ""
}

// detectOverrides inspects each key in keys and returns a configOverride for
// every key that (a) is present in the config file (fv) and (b) is set to a
// different value by an env var from dotenv or process.
func detectOverrides(fv *viper.Viper, dotenv, process map[string]string, keys []string) []configOverride {
	var overrides []configOverride
	for _, key := range keys {
		if !fv.IsSet(key) {
			continue
		}
		envKey := envKeyForConfigKey(key)
		envVal, source := envOverrideSource(envKey, dotenv, process)
		if source == "" {
			continue
		}
		fileVal := normalizeViperValue(fv.Get(key))
		if fileVal == envVal {
			continue
		}
		overrides = append(overrides, configOverride{
			Key:            key,
			EnvVar:         envKey,
			EffectiveValue: maskIfSensitive(key, envVal),
			FileValue:      maskIfSensitive(key, fileVal),
			Source:         source,
		})
	}
	return overrides
}

// logConfigOverrides emits one slog.Info line per override describing which
// env var overrode which config-file value.
func logConfigOverrides(overrides []configOverride) {
	for _, o := range overrides {
		slog.Info("config key overridden",
			"key", o.Key,
			"env_var", o.EnvVar,
			"effective_value", o.EffectiveValue,
			"file_value", o.FileValue,
			"source", o.Source,
		)
	}
}
