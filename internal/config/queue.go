package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultTaskDeadline            = 10 * time.Minute
	DefaultPollMaxInterval         = 10 * time.Second
	DefaultMaxToolIterations       = 10
	DefaultTokenBudget             = 0
	DefaultAgenticTruncatorMax     = 30
	DefaultAgenticTruncationThreshold = 40
)

type QueueConfig struct {
	TaskDeadline               time.Duration
	PollMaxInterval            time.Duration
	MaxToolIterations          int
	TokenBudget                int
	AgenticTruncatorMax        int
	AgenticTruncationThreshold int
}

func setQueueDefaults(v *viper.Viper) {
	v.SetDefault("queue.task_deadline", DefaultTaskDeadline.String())
	v.SetDefault("queue.poll_max_interval", DefaultPollMaxInterval.String())
	v.SetDefault("queue.max_tool_iterations", DefaultMaxToolIterations)
	v.SetDefault("queue.token_budget", DefaultTokenBudget)
	v.SetDefault("queue.agentic_truncator_max", DefaultAgenticTruncatorMax)
	v.SetDefault("queue.agentic_truncation_threshold", DefaultAgenticTruncationThreshold)
}

func loadQueueConfig(v *viper.Viper) QueueConfig {
	return QueueConfig{
		TaskDeadline:               v.GetDuration("queue.task_deadline"),
		PollMaxInterval:            v.GetDuration("queue.poll_max_interval"),
		MaxToolIterations:          v.GetInt("queue.max_tool_iterations"),
		TokenBudget:                v.GetInt("queue.token_budget"),
		AgenticTruncatorMax:        v.GetInt("queue.agentic_truncator_max"),
		AgenticTruncationThreshold: v.GetInt("queue.agentic_truncation_threshold"),
	}
}
