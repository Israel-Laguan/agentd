package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	// DefaultToolTimeout is the fallback timeout for any tool without an
	// explicit entry in ToolTimeouts.
	DefaultToolTimeout = 30 * time.Second

	// DefaultBashToolTimeout is the default timeout for bash tool calls.
	DefaultBashToolTimeout = 60 * time.Second

	// DefaultReadToolTimeout is the default timeout for read tool calls.
	DefaultReadToolTimeout = 10 * time.Second

	// DefaultWriteToolTimeout is the default timeout for write tool calls.
	DefaultWriteToolTimeout = 10 * time.Second

	// DefaultDelegateToolTimeout is the default timeout for delegate tool
	// calls. Delegation spawns sub-agents that run full agentic loops, so
	// the timeout must be generous (aligned with the task deadline).
	DefaultDelegateToolTimeout = 10 * time.Minute
)

// ToolTimeoutsConfig maps tool names to per-tool timeout durations.
// The special key "default" sets the fallback for unlisted tools.
type ToolTimeoutsConfig struct {
	Defaults map[string]time.Duration
}

// Lookup returns the timeout for the given tool name. It checks for an
// exact match first, then returns the "default" entry. If neither is
// present it returns fallback.
func (c ToolTimeoutsConfig) Lookup(toolName string, fallback time.Duration) time.Duration {
	if d, ok := c.Defaults[toolName]; ok {
		return d
	}
	if d, ok := c.Defaults["default"]; ok {
		return d
	}
	return fallback
}

func setToolTimeoutDefaults(v *viper.Viper) {
	v.SetDefault("queue.tool_timeouts.bash", DefaultBashToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.read", DefaultReadToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.write", DefaultWriteToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.delegate", DefaultDelegateToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.delegate_parallel", DefaultDelegateToolTimeout.String())
	v.SetDefault("queue.tool_timeouts.default", DefaultToolTimeout.String())
}

func defaultToolTimeout(k string) time.Duration {
	switch k {
	case "bash":
		return DefaultBashToolTimeout
	case "read":
		return DefaultReadToolTimeout
	case "write":
		return DefaultWriteToolTimeout
	case "delegate", "delegate_parallel":
		return DefaultDelegateToolTimeout
	default:
		return DefaultToolTimeout
	}
}

func loadToolTimeoutsConfig(v *viper.Viper) ToolTimeoutsConfig {
	knownKeys := []string{"bash", "read", "write", "delegate", "delegate_parallel", "default"}
	result := ToolTimeoutsConfig{Defaults: make(map[string]time.Duration, len(knownKeys))}
	for _, k := range knownKeys {
		key := "queue.tool_timeouts." + k
		fallback := defaultToolTimeout(k)
		if d := parseViperDuration(v, key, fallback, time.Second); d > 0 {
			result.Defaults[k] = d
		}
	}
	raw := v.GetStringMap("queue.tool_timeouts")
	for k := range raw {
		if _, exists := result.Defaults[k]; exists {
			continue
		}
		key := "queue.tool_timeouts." + k
		if d := parseViperDuration(v, key, 0, time.Second); d > 0 {
			result.Defaults[k] = d
		}
	}
	return result
}
