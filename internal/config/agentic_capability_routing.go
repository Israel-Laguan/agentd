package config

import "github.com/spf13/viper"

const DefaultCapabilityRoutingMinConfidence = 0.35

// CapabilityRoutingConfig maps classified task intents to external capability adapters.
type CapabilityRoutingConfig struct {
	Enabled       bool
	MinConfidence float64
	// Mappings maps intent names (generate_image, real_time_search, …) to adapter registry names.
	Mappings map[string]string
	// Tools maps intent names to tool names; when omitted for an intent, the intent name is used.
	Tools map[string]string
}

func setCapabilityRoutingDefaults(v *viper.Viper) {
	v.SetDefault("agentic.capability_routing.enabled", false)
	v.SetDefault("agentic.capability_routing.min_confidence", DefaultCapabilityRoutingMinConfidence)
}

func loadCapabilityRoutingConfig(v *viper.Viper) CapabilityRoutingConfig {
	minConf := v.GetFloat64("agentic.capability_routing.min_confidence")
	if minConf < 0 {
		minConf = 0
	}
	if minConf > 1 {
		minConf = 1
	}
	if minConf == 0 && !v.IsSet("agentic.capability_routing.min_confidence") {
		minConf = DefaultCapabilityRoutingMinConfidence
	}
	return CapabilityRoutingConfig{
		Enabled:       v.GetBool("agentic.capability_routing.enabled"),
		MinConfidence: minConf,
		Mappings:      loadCapabilityRoutingMappings(v),
		Tools:         loadCapabilityRoutingTools(v),
	}
}

func loadCapabilityRoutingMappings(v *viper.Viper) map[string]string {
	return loadStringStringMap(v, "agentic.capability_routing.mappings")
}

func loadCapabilityRoutingTools(v *viper.Viper) map[string]string {
	return loadStringStringMap(v, "agentic.capability_routing.tools")
}

func loadStringStringMap(v *viper.Viper, key string) map[string]string {
	if !v.IsSet(key) {
		return nil
	}
	raw := v.Get(key)
	if raw == nil {
		return nil
	}
	switch m := raw.(type) {
	case map[string]interface{}:
		out := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				out[k] = s
			}
		}
		return out
	case map[string]string:
		cp := make(map[string]string, len(m))
		for k, val := range m {
			cp[k] = val
		}
		return cp
	default:
		return nil
	}
}
