package queue

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/testutil"
)

func TestBootstrapFromConfigSkipsEmptyID(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	if err := s.BootstrapFromConfig(context.Background(), config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{ID: "  ", CronExpr: "0 * * * *", ContextFn: "static"},
			{ID: "ok", CronExpr: "0 * * * *", ContextFn: "static"},
		},
	}); err != nil {
		t.Fatalf("BootstrapFromConfig: %v", err)
	}
	if len(store.ScheduledTasks()) != 1 {
		t.Fatalf("scheduled entries = %d, want 1", len(store.ScheduledTasks()))
	}
}

func TestBootstrapFromConfigUpsertsTasks(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})

	runAfter := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	cfg := config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{
				ID:          "cron-job",
				CronExpr:    "0 * * * *",
				TaskType:    "summarize",
				ContextFn:   "static",
				ContextArgs: map[string]string{"body": "cron body"},
				Title:       "Cron Job",
			},
			{
				ID:          "once-job",
				RunAfter:    runAfter.Format(time.RFC3339),
				ContextFn:   "static",
				ContextArgs: map[string]string{"body": "once body"},
				Title:       "Once Job",
			},
		},
	}
	if err := s.BootstrapFromConfig(ctx, cfg); err != nil {
		t.Fatalf("BootstrapFromConfig: %v", err)
	}
	entries := store.ScheduledTasks()
	if len(entries) != 2 {
		t.Fatalf("scheduled entries = %d, want 2", len(entries))
	}
	if _, ok := s.cronByID["cron-job"]; !ok {
		t.Fatal("cronByID missing cron-job")
	}
	if _, ok := s.cronByID["once-job"]; ok {
		t.Fatal("cronByID should not cache one-shot without cron_expr")
	}
	for _, e := range entries {
		if e.OutputTarget != config.DefaultSchedulerOutputTarget {
			t.Fatalf("entry %q OutputTarget = %q, want %q", e.ID, e.OutputTarget, config.DefaultSchedulerOutputTarget)
		}
	}
}

func TestBootstrapFromConfigInvalidCron(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	err := s.BootstrapFromConfig(context.Background(), config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{ID: "bad", CronExpr: "not valid cron", ContextFn: "static"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "cron") {
		t.Fatalf("BootstrapFromConfig() err = %v, want cron error", err)
	}
}

func TestBootstrapFromConfigInvalidRunAfter(t *testing.T) {
	store := testutil.NewFakeStore()
	s := NewScheduler(store, nil, SchedulerOptions{Enabled: true})
	err := s.BootstrapFromConfig(context.Background(), config.SchedulerConfig{
		Tasks: []config.SchedulerTaskConfig{
			{ID: "bad", RunAfter: "not-a-time", ContextFn: "static"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "run_after") {
		t.Fatalf("BootstrapFromConfig() err = %v, want run_after error", err)
	}
}

func TestNewSchedulerFromConfig(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	agentic := config.AgenticConfig{
		Scheduler: config.SchedulerConfig{
			Enabled: true,
			Tasks: []config.SchedulerTaskConfig{
				{
					ID:        "boot",
					CronExpr:  "0 * * * *",
					ContextFn: "static",
					Title:     "Boot",
				},
			},
		},
	}
	s, err := NewSchedulerFromConfig(ctx, store, nil, agentic, config.LibrarianConfig{})
	if err != nil {
		t.Fatalf("NewSchedulerFromConfig: %v", err)
	}
	if s == nil || !s.Enabled() {
		t.Fatal("expected enabled scheduler")
	}
	if len(store.ScheduledTasks()) != 1 {
		t.Fatalf("scheduled tasks = %d, want 1", len(store.ScheduledTasks()))
	}
}

func TestNewSchedulerFromConfigRequiresScheduledStore(t *testing.T) {
	_, err := NewSchedulerFromConfig(
		context.Background(),
		nil,
		nil,
		config.AgenticConfig{Scheduler: config.SchedulerConfig{Enabled: true}},
		config.LibrarianConfig{},
	)
	if err == nil || !strings.Contains(err.Error(), "ScheduledTaskStore") {
		t.Fatalf("NewSchedulerFromConfig() err = %v, want ScheduledTaskStore error", err)
	}
}
