package config

import (
	"testing"
	"time"
)

func TestQueueConfig_Defaults(t *testing.T) {
	cfg := QueueConfig{
		TaskDeadline:    DefaultTaskDeadline,
		PollMaxInterval: DefaultPollMaxInterval,
	}
	if cfg.TaskDeadline != 10*time.Minute {
		t.Errorf("TaskDeadline = %v, want 10m", cfg.TaskDeadline)
	}
	if cfg.PollMaxInterval != 10*time.Second {
		t.Errorf("PollMaxInterval = %v, want 10s", cfg.PollMaxInterval)
	}
}

func TestQueueConfig_Custom(t *testing.T) {
	cfg := QueueConfig{
		TaskDeadline:    30 * time.Minute,
		PollMaxInterval: 5 * time.Second,
	}
	if cfg.TaskDeadline != 30*time.Minute {
		t.Errorf("TaskDeadline = %v, want 30m", cfg.TaskDeadline)
	}
	if cfg.PollMaxInterval != 5*time.Second {
		t.Errorf("PollMaxInterval = %v, want 5s", cfg.PollMaxInterval)
	}
}

func TestDefaultQueueValues(t *testing.T) {
	if DefaultTaskDeadline != 10*time.Minute {
		t.Errorf("DefaultTaskDeadline = %v, want 10m", DefaultTaskDeadline)
	}
	if DefaultPollMaxInterval != 10*time.Second {
		t.Errorf("DefaultPollMaxInterval = %v, want 10s", DefaultPollMaxInterval)
	}
}