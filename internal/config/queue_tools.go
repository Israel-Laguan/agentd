package config

import (
	"math"
	"strconv"
	"strings"
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

const (
	// DefaultToolRetryMaxAttempts is the default number of retry attempts
	// for tool-level transient failures.
	DefaultToolRetryMaxAttempts = 3

	// DefaultToolRetryBaseDelay is the base delay between tool retry attempts.
	DefaultToolRetryBaseDelay = 200 * time.Millisecond

	// DefaultToolRetryMaxDelay is the ceiling for exponential backoff.
	DefaultToolRetryMaxDelay = 5 * time.Second
)

// ToolRetriesConfig controls tool-level retry behaviour for transient errors.
type ToolRetriesConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Tools       map[string]struct{}
}

// Allows reports whether transparent retries are enabled for the given tool.
func (c ToolRetriesConfig) Allows(toolName string) bool {
	if len(c.Tools) == 0 {
		return false
	}
	_, ok := c.Tools[toolName]
	return ok
}

func setToolRetryDefaults(v *viper.Viper) {
	v.SetDefault("queue.tool_retries.max_attempts", DefaultToolRetryMaxAttempts)
	v.SetDefault("queue.tool_retries.base_delay", DefaultToolRetryBaseDelay.String())
	v.SetDefault("queue.tool_retries.max_delay", DefaultToolRetryMaxDelay.String())
	v.SetDefault("queue.tool_retries.tools", []string{"read"})
}

func loadToolRetriesConfig(v *viper.Viper) ToolRetriesConfig {
	cfg := ToolRetriesConfig{
		MaxAttempts: v.GetInt("queue.tool_retries.max_attempts"),
		BaseDelay:   parseViperDuration(v, "queue.tool_retries.base_delay", DefaultToolRetryBaseDelay, time.Millisecond),
		MaxDelay:    parseViperDuration(v, "queue.tool_retries.max_delay", DefaultToolRetryMaxDelay, time.Millisecond),
	}
	tools := v.GetStringSlice("queue.tool_retries.tools")
	if len(tools) > 0 {
		cfg.Tools = make(map[string]struct{}, len(tools))
		for _, name := range tools {
			cfg.Tools[name] = struct{}{}
		}
	}
	return cfg
}

// parseViperDuration reads key when IsSet; otherwise returns fallback.
// String values use time.ParseDuration (e.g. "200ms", "60s").
// Bare numeric values are multiplied by bareNumberUnit (milliseconds for retries, seconds for timeouts).
func parseViperDuration(v *viper.Viper, key string, fallback, bareNumberUnit time.Duration) time.Duration {
	if !v.IsSet(key) {
		return fallback
	}
	switch raw := v.Get(key).(type) {
	case string:
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return fallback
		}
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			return d
		}
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fallback
		}
		return scaleBareNumber(n, bareNumberUnit, fallback)
	case int:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case int64:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case int32:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case float64:
		return scaleBareNumber(raw, bareNumberUnit, fallback)
	case float32:
		return scaleBareNumber(float64(raw), bareNumberUnit, fallback)
	case time.Duration:
		if raw > 0 {
			return raw
		}
		return fallback
	default:
		return fallback
	}
}

func scaleBareNumber(n float64, unit, fallback time.Duration) time.Duration {
	if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return fallback
	}
	scaled := n * float64(unit)
	if scaled <= 0 || math.IsNaN(scaled) || math.IsInf(scaled, 0) || scaled > float64(math.MaxInt64) {
		return fallback
	}
	return time.Duration(scaled)
}
