package config

import (
	"time"

	"github.com/spf13/viper"
)

const (
	defaultTaskDeadline     = 10 * time.Minute
	defaultPollMaxInterval  = 10 * time.Second
)

type QueueConfig struct {
	TaskDeadline    time.Duration
	PollMaxInterval time.Duration
}

func setQueueDefaults(v *viper.Viper) {
	v.SetDefault("queue.task_deadline", defaultTaskDeadline.String())
	v.SetDefault("queue.poll_max_interval", defaultPollMaxInterval.String())
}

func loadQueueConfig(v *viper.Viper) QueueConfig {
	return QueueConfig{
		TaskDeadline:    v.GetDuration("queue.task_deadline"),
		PollMaxInterval: v.GetDuration("queue.poll_max_interval"),
	}
}
