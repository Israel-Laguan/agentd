package config

import "github.com/spf13/viper"

const DefaultToolManifestMinConfidence = 0.35

// ToolManifestConfig controls per-task tool list filtering for agentic runs.
type ToolManifestConfig struct {
	Enabled       bool
	MinConfidence float64
	// Mappings maps task type names (summarize, code_gen, doc_qa, web_research, full_agent)
	// to tool names advertised to the model. Empty slice means no tools; "*" or nil means all tools.
	Mappings map[string][]string
}

func setToolManifestDefaults(v *viper.Viper) {
	v.SetDefault("agentic.tool_manifest.enabled", false)
	v.SetDefault("agentic.tool_manifest.min_confidence", DefaultToolManifestMinConfidence)
}

func loadToolManifestConfig(v *viper.Viper) ToolManifestConfig {
	return ToolManifestConfig{
		Enabled:       v.GetBool("agentic.tool_manifest.enabled"),
		MinConfidence: loadConfidence(v, "agentic.tool_manifest.min_confidence", DefaultToolManifestMinConfidence),
		Mappings:      loadToolManifestMappings(v),
	}
}

func loadToolManifestMappings(v *viper.Viper) map[string][]string {
	const key = "agentic.tool_manifest.mappings"
	if !v.IsSet(key) {
		return nil
	}
	raw := v.Get(key)
	if raw == nil {
		return nil
	}
	switch m := raw.(type) {
	case map[string]interface{}:
		out := make(map[string][]string, len(m))
		for k, val := range m {
			out[k] = interfaceToStringSlice(val)
		}
		return out
	case map[string][]string:
		cp := make(map[string][]string, len(m))
		for k, val := range m {
			cp[k] = append([]string(nil), val...)
		}
		return cp
	default:
		return nil
	}
}

func interfaceToStringSlice(v interface{}) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []interface{}:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

const DefaultCapabilityRoutingMinConfidence = 0.35

// CapabilityRoutingConfig maps classified task intents to external capability adapters.
type CapabilityRoutingConfig struct {
	Enabled       bool
	MinConfidence float64
	// Mappings maps intent names (generate_image, real_time_search, ...) to adapter registry names.
	Mappings map[string]string
	// Tools maps intent names to tool names; when omitted for an intent, the intent name is used.
	Tools map[string]string
}

func setCapabilityRoutingDefaults(v *viper.Viper) {
	v.SetDefault("agentic.capability_routing.enabled", false)
	v.SetDefault("agentic.capability_routing.min_confidence", DefaultCapabilityRoutingMinConfidence)
}

func loadCapabilityRoutingConfig(v *viper.Viper) CapabilityRoutingConfig {
	return CapabilityRoutingConfig{
		Enabled:       v.GetBool("agentic.capability_routing.enabled"),
		MinConfidence: loadConfidence(v, "agentic.capability_routing.min_confidence", DefaultCapabilityRoutingMinConfidence),
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

func loadConfidence(v *viper.Viper, key string, fallback float64) float64 {
	confidence := v.GetFloat64(key)
	if confidence < 0 {
		return 0
	}
	if confidence > 1 {
		return 1
	}
	if confidence == 0 && !v.IsSet(key) {
		return fallback
	}
	return confidence
}
