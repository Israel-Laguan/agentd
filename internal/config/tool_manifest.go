package config

import (
	"encoding/json"
	"log/slog"

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
				if err := json.Unmarshal([]byte(m), &parsed); err != nil {
					slog.Warn("invalid agentic.tool_manifest.mappings JSON", "err", err)
				} else {
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
