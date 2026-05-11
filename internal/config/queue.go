package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultTaskDeadline      = 10 * time.Minute
	defaultPollMaxInterval   = 10 * time.Second
	defaultMaxToolIterations = 10
	defaultTokenBudget       = 0
)

type QueueConfig struct {
	TaskDeadline     time.Duration
	PollMaxInterval  time.Duration
	MaxToolIterations int
	TokenBudget      int
}

func setQueueDefaults(v *viper.Viper) {
	v.SetDefault("queue.task_deadline", defaultTaskDeadline.String())
	v.SetDefault("queue.poll_max_interval", defaultPollMaxInterval.String())
	v.SetDefault("queue.max_tool_iterations", defaultMaxToolIterations)
	v.SetDefault("queue.token_budget", defaultTokenBudget)
}

func loadQueueConfig(v *viper.Viper) QueueConfig {
	return QueueConfig{
		TaskDeadline:      v.GetDuration("queue.task_deadline"),
		PollMaxInterval:   v.GetDuration("queue.poll_max_interval"),
		MaxToolIterations: v.GetInt("queue.max_tool_iterations"),
		TokenBudget:       v.GetInt("queue.token_budget"),
	}
}
