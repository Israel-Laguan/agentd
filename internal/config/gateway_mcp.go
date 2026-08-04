package config

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/viper"
)

type AuthConfig struct {
	Type  string `json:"type" mapstructure:"type"`
	Token string `json:"token" mapstructure:"token"`
}

type CapabilityManifest struct {
	Type      string     `json:"type" mapstructure:"type"`
	Name      string     `json:"name" mapstructure:"name"`
	ServerURL string     `json:"server_url,omitempty" mapstructure:"server_url"`
	Command   string     `json:"command,omitempty" mapstructure:"command"`
	Args      []string   `json:"args,omitempty" mapstructure:"args"`
	Auth      AuthConfig `json:"auth,omitempty" mapstructure:"auth"`
}

func loadMCPServers(v *viper.Viper) ([]CapabilityManifest, error) {
	if v.IsSet("gateway.mcp_servers") {
		var caps []CapabilityManifest
		if err := v.UnmarshalKey("gateway.mcp_servers", &caps); err != nil {
			return nil, fmt.Errorf("invalid gateway.mcp_servers: %w", err)
		}
		expandAuthTokens(caps)
		return caps, nil
	}

	// Backward compat: fall back to the old gateway.capabilities key, deprecated in favour of
	// gateway.mcp_servers. Will be removed in a future release.
	var legacy []CapabilityManifest
	if err := v.UnmarshalKey("gateway.capabilities", &legacy); err != nil {
		return nil, fmt.Errorf("invalid gateway.capabilities: %w", err)
	}
	expandAuthTokens(legacy)
	if len(legacy) > 0 {
		slog.Warn("gateway.capabilities is deprecated; rename the config key to gateway.mcp_servers")
	}
	return legacy, nil
}

// expandAuthTokens expands environment variable references in MCP auth tokens in place.
func expandAuthTokens(caps []CapabilityManifest) {
	for i := range caps {
		if caps[i].Auth.Token != "" {
			caps[i].Auth.Token = os.ExpandEnv(caps[i].Auth.Token)
		}
	}
}
