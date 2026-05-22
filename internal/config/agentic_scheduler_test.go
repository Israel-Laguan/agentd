package config

import (
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestLoadSchedulerConfigDefaults(t *testing.T) {
	v := viper.New()
	setSchedulerDefaults(v)
	cfg := loadSchedulerConfig(v)
	if cfg.Enabled {
		t.Fatal("expected scheduler disabled by default")
	}
	if cfg.TickInterval != DefaultSchedulerTickInterval {
		t.Fatalf("TickInterval = %s, want %s", cfg.TickInterval, DefaultSchedulerTickInterval)
	}
}

func TestLoadSchedulerConfigParsesTasks(t *testing.T) {
	v := viper.New()
	v.Set("agentic.scheduler.enabled", true)
	v.Set("agentic.scheduler.tick_interval", "2m")
	v.Set("agentic.scheduler.tasks", []map[string]interface{}{
		{
			"id":          "job",
			"cron_expr":   "*/5 * * * *",
			"task_type":   "summarize",
			"context_fn":  "static",
			"context_args": map[string]interface{}{"body": "hi"},
			"title":       "Job",
		},
	})
	cfg := loadSchedulerConfig(v)
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.TickInterval != 2*time.Minute {
		t.Fatalf("TickInterval = %s, want 2m", cfg.TickInterval)
	}
	if len(cfg.Tasks) != 1 || cfg.Tasks[0].ID != "job" {
		t.Fatalf("tasks = %#v", cfg.Tasks)
	}
}
