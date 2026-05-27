package config

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/spf13/viper"
)

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

// detectOverrides inspects each key in keys and returns a configOverride when
// the config file sets a value that differs from an env/dotenv var and the
// effective resolved value (v) matches the env value (env actually won).
func detectOverrides(fv, v *viper.Viper, dotenv, process map[string]string, keys []string) []configOverride {
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
		effective := normalizeViperValue(v.Get(key))
		if effective != envVal {
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

// isMonitoredGatewayFlatKey reports legacy gateway.<provider>.api_key|base_url keys.
func isMonitoredGatewayFlatKey(key string) bool {
	parts := strings.Split(key, ".")
	if len(parts) != 3 || parts[0] != "gateway" {
		return false
	}
	switch parts[2] {
	case "api_key", "base_url":
		return parts[1] != "providers"
	default:
		return false
	}
}

// monitoredGatewayFlatKeys returns gateway.order plus any gateway.<provider>.{api_key,base_url}
// keys present in the config file snapshot.
func monitoredGatewayFlatKeys(fv *viper.Viper) []string {
	if fv == nil {
		return []string{"gateway.order"}
	}
	seen := map[string]struct{}{"gateway.order": {}}
	for _, key := range fv.AllKeys() {
		if isMonitoredGatewayFlatKey(key) {
			seen[key] = struct{}{}
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// detectAllConfigOverrides merges flat gateway key overrides with gateway.providers overrides.
func detectAllConfigOverrides(fv, v *viper.Viper, dotenv, process map[string]string) []configOverride {
	flat := detectOverrides(fv, v, dotenv, process, monitoredGatewayFlatKeys(fv))
	provider := detectProviderEntryOverrides(fv, v, dotenv, process)
	if len(provider) == 0 {
		return flat
	}
	out := make([]configOverride, 0, len(flat)+len(provider))
	out = append(out, flat...)
	out = append(out, provider...)
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// logConfigOverrides emits one slog.Info line per override describing which
// env var overrode which config-file value.
func logConfigOverrides(overrides []configOverride) {
	for _, o := range overrides {
		slog.Info("config: key overridden by env",
			"key", o.Key,
			"env_var", o.EnvVar,
			"effective_value", o.EffectiveValue,
			"file_value", o.FileValue,
			"source", o.Source,
		)
	}
}
