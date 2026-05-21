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
	minConf := v.GetFloat64("agentic.tool_manifest.min_confidence")
	if minConf < 0 {
		minConf = 0
	}
	if minConf > 1 {
		minConf = 1
	}
	if minConf == 0 && !v.IsSet("agentic.tool_manifest.min_confidence") {
		minConf = DefaultToolManifestMinConfidence
	}
	return ToolManifestConfig{
		Enabled:       v.GetBool("agentic.tool_manifest.enabled"),
		MinConfidence: minConf,
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
