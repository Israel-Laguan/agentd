package config

import "github.com/spf13/viper"

// MCPConfig controls the MCP board-export server.
type MCPConfig struct {
	// Enabled gates the MCP server. Default: false.
	Enabled bool
	// Transport selects the transport protocol: "stdio", "http", or "both".
	Transport string
	// HTTPAddr is the listen address when transport includes "http".
	// Defaults to "127.0.0.1:0" (loopback, random port).
	HTTPAddr string
}

func setMCPDefaults(v *viper.Viper) {
	v.SetDefault("mcp.enabled", false)
	v.SetDefault("mcp.transport", "stdio")
	v.SetDefault("mcp.http_addr", "127.0.0.1:0")
}

func loadMCPConfig(v *viper.Viper) MCPConfig {
	return MCPConfig{
		Enabled:   v.GetBool("mcp.enabled"),
		Transport: v.GetString("mcp.transport"),
		HTTPAddr:  v.GetString("mcp.http_addr"),
	}
}
