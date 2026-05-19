package config

import "github.com/spf13/viper"

// AgenticConfig holds agentic-mode worker settings from the top-level agentic: block.
type AgenticConfig struct {
	// ToolCredentials maps tool names to environment variable names holding credentials.
	ToolCredentials map[string]string
}

func loadAgenticConfig(v *viper.Viper) AgenticConfig {
	return AgenticConfig{
		ToolCredentials: v.GetStringMapString("agentic.tool_credentials"),
	}
}
