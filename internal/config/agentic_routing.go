package config

import (
	"encoding/json"

	"github.com/spf13/viper"
)

const DefaultToolManifestMinConfidence = 0.35

// ToolManifestConfig controls per-task tool list filtering for agentic runs.
type ToolManifestConfig struct {
	Enabled       bool
	MinConfidence float64
	// Mappings maps task type names (summarize, code_gen, doc_qa, web_research, full_agent)
	// to tool names advertised to the model. Empty slice means no tools; "*" or nil means all tools.
	Mappings map[string][]string
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

func loadToolManifestConfig(v *viper.Viper) ToolManifestConfig {
	tmEnabled := v.GetBool("agentic.tool_manifest.enabled")
	tmMin := v.GetFloat64("agentic.tool_manifest.min_confidence")
	if tmMin < 0 {
		tmMin = 0
	}
	if tmMin > 1 {
		tmMin = 1
	}
	if tmMin == 0 && !v.IsSet("agentic.tool_manifest.min_confidence") {
		tmMin = DefaultToolManifestMinConfidence
	}
	var tmMappings map[string][]string
	if v.IsSet("agentic.tool_manifest.mappings") {
		if raw := v.Get("agentic.tool_manifest.mappings"); raw != nil {
			switch m := raw.(type) {
			case string:
				var parsed map[string][]string
				if json.Unmarshal([]byte(m), &parsed) == nil {
					tmMappings = parsed
				}
			case map[string]interface{}:
				tmMappings = make(map[string][]string, len(m))
				for k, val := range m {
					tmMappings[k] = interfaceToStringSlice(val)
				}
			case map[string][]string:
				tmMappings = make(map[string][]string, len(m))
				for k, val := range m {
					tmMappings[k] = append([]string(nil), val...)
				}
			}
		}
	}
	return ToolManifestConfig{
		Enabled:       tmEnabled,
		MinConfidence: tmMin,
		Mappings:      tmMappings,
	}
}

func loadCapabilityRoutingConfig(v *viper.Viper) CapabilityRoutingConfig {
	crEnabled := v.GetBool("agentic.capability_routing.enabled")
	crMin := v.GetFloat64("agentic.capability_routing.min_confidence")
	if crMin < 0 {
		crMin = 0
	}
	if crMin > 1 {
		crMin = 1
	}
	if crMin == 0 && !v.IsSet("agentic.capability_routing.min_confidence") {
		crMin = DefaultCapabilityRoutingMinConfidence
	}
	crMappings := loadStringStringMap(v, "agentic.capability_routing.mappings")
	crTools := loadStringStringMap(v, "agentic.capability_routing.tools")
	return CapabilityRoutingConfig{
		Enabled:       crEnabled,
		MinConfidence: crMin,
		Mappings:      crMappings,
		Tools:         crTools,
	}
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
	case string:
		var out map[string]string
		if json.Unmarshal([]byte(m), &out) == nil {
			return out
		}
		return nil
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

// interfaceToStringSlice normalizes a viper-unmarshaled value (from yaml/json etc)
// into []string for ToolManifest mappings. Supports []string, []interface{}, and nil.
func interfaceToStringSlice(v interface{}) []string {
	switch s := v.(type) {
	case nil:
		return nil
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
