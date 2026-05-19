package config

import "github.com/spf13/viper"

// AgenticConfig holds agent-loop settings documented under the top-level
// agentic: key in config.reference.yaml.
type AgenticConfig struct {
	// ExternalTools lists tool names whose results are wrapped in
	// <external_content> tags. When empty, all non-builtin tools are wrapped.
	ExternalTools []string
	// ToolCredentials maps tool names to environment variable names holding credentials.
	ToolCredentials map[string]string
}

func setAgenticDefaults(v *viper.Viper) {
	v.SetDefault("agentic.external_tools", []string{})
}

func loadAgenticConfig(v *viper.Viper) AgenticConfig {
	return AgenticConfig{
		ExternalTools:   v.GetStringSlice("agentic.external_tools"),
		ToolCredentials: v.GetStringMapString("agentic.tool_credentials"),
	}
}
