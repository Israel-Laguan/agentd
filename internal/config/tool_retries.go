package config

import (
	"time"

	"github.com/spf13/viper"
)

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
