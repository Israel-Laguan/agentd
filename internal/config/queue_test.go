package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestQueueDefaults(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	cfg := loadQueueConfig(v)

	if cfg.TaskDeadline != DefaultTaskDeadline {
		t.Fatalf("TaskDeadline = %v, want %v", cfg.TaskDeadline, DefaultTaskDeadline)
	}
	if cfg.QueuedReconcileAfter != DefaultQueuedReconcileAfter {
		t.Fatalf("QueuedReconcileAfter = %v, want %v", cfg.QueuedReconcileAfter, DefaultQueuedReconcileAfter)
	}
}

func TestQueueConfigOverride_QueuedReconcileAfter(t *testing.T) {
	v := viper.New()
	setQueueDefaults(v)
	v.Set("queue.task_deadline", "1m")
	v.Set("queue.queued_reconcile_after", "15m")
	cfg := loadQueueConfig(v)

	if cfg.TaskDeadline != time.Minute {
		t.Fatalf("TaskDeadline = %v, want 1m", cfg.TaskDeadline)
	}
	if cfg.QueuedReconcileAfter != 15*time.Minute {
		t.Fatalf("QueuedReconcileAfter = %v, want 15m", cfg.QueuedReconcileAfter)
	}
}

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
	if DefaultQueuedReconcileAfter != 10*time.Minute {
		t.Errorf("DefaultQueuedReconcileAfter = %v, want 10m", DefaultQueuedReconcileAfter)
	}
	if DefaultPollMaxInterval != 10*time.Second {
		t.Errorf("DefaultPollMaxInterval = %v, want 10s", DefaultPollMaxInterval)
	}
}