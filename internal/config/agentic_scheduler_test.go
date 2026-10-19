package config

import (
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestLoadSchedulerConfigDefaults(t *testing.T) {
	v := viper.New()
	setSchedulerDefaults(v)
	cfg, err := loadSchedulerConfig(v)
	if err != nil {
		t.Fatalf("loadSchedulerConfig: %v", err)
	}
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
			"id":           "job",
			"cron_expr":    "*/5 * * * *",
			"task_type":    "summarize",
			"context_fn":   "static",
			"context_args": map[string]interface{}{"body": "hi"},
			"title":        "Job",
			"project_id":   "p1",
		},
	})
	cfg, err := loadSchedulerConfig(v)
	if err != nil {
		t.Fatalf("loadSchedulerConfig: %v", err)
	}
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.TickInterval != 2*time.Minute {
		t.Fatalf("TickInterval = %s, want 2m", cfg.TickInterval)
	}
	if len(cfg.Tasks) != 1 || cfg.Tasks[0].ID != "job" {
		t.Fatalf("tasks = %#v", cfg.Tasks)
	}
	if cfg.Tasks[0].OutputTarget != DefaultSchedulerOutputTarget {
		t.Fatalf("OutputTarget = %q, want %q", cfg.Tasks[0].OutputTarget, DefaultSchedulerOutputTarget)
	}
}

func TestLoadSchedulerConfigInvalidTasksReturnsError(t *testing.T) {
	v := viper.New()
	setSchedulerDefaults(v)
	v.Set("agentic.scheduler.tasks", "not-a-list")

	_, err := loadSchedulerConfig(v)
	if err == nil {
		t.Fatal("expected error for invalid agentic.scheduler.tasks")
	}
}

func TestLoadSchedulerConfigRejectsTaskWithoutProject(t *testing.T) {
	task := func(projectID string) map[string]interface{} {
		m := map[string]interface{}{"id": "s1", "run_after": "2020-01-01T00:00:00Z", "title": "t"}
		if projectID != "" {
			m["project_id"] = projectID
		}
		return m
	}
	tests := []struct {
		name      string
		enabled   bool
		schedProj string
		taskProj  string
		wantErr   bool
	}{
		{"enabled, no project anywhere", true, "", "", true},
		{"enabled, scheduler project", true, "p1", "", false},
		{"enabled, task project", true, "", "p2", false},
		{"disabled, no project", false, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := viper.New()
			setSchedulerDefaults(v)
			v.Set("agentic.scheduler.enabled", tc.enabled)
			v.Set("agentic.scheduler.project_id", tc.schedProj)
			v.Set("agentic.scheduler.tasks", []map[string]interface{}{task(tc.taskProj)})
			_, err := loadSchedulerConfig(v)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), "s1") {
				t.Fatalf("error should name the task: %v", err)
			}
		})
	}
}
