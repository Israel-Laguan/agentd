package testutil

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestFakeInsertReadyTaskMissingScheduleNoPartialTask(t *testing.T) {
	store := NewFakeStore()
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	slot := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	_, err = store.InsertReadyTaskAndRecordDispatch(ctx, project.ID, models.DraftTask{
		Title: "Orphan dispatch",
	}, "missing-schedule", slot, false)
	if err == nil {
		t.Fatal("InsertReadyTaskAndRecordDispatch: want error")
	}
	if !strings.Contains(err.Error(), "missing-schedule") {
		t.Fatalf("error = %v, want schedule id", err)
	}
	if len(store.Tasks()) != 0 {
		t.Fatalf("tasks = %d, want 0", len(store.Tasks()))
	}
}

func TestFakeUpsertScheduledTaskPreservesLastFiredOnConflict(t *testing.T) {
	store := NewFakeStore()
	ctx := context.Background()

	fired := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	entry := models.ScheduledTask{
		ID:          "health",
		CronExpr:    "*/5 * * * *",
		Title:       "Health",
		ContextFn:   "static",
		Kind:        models.ScheduledTaskKindDispatch,
		Enabled:     true,
		LastFiredAt: &fired,
	}
	if err := store.UpsertScheduledTask(ctx, entry); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	if err := store.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID:        entry.ID,
		CronExpr:  entry.CronExpr,
		Title:     entry.Title,
		ContextFn: entry.ContextFn,
		Kind:      entry.Kind,
		Enabled:   entry.Enabled,
	}); err != nil {
		t.Fatalf("bootstrap upsert: %v", err)
	}

	tasks, err := store.ListScheduledTasks(ctx)
	if err != nil {
		t.Fatalf("ListScheduledTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	if tasks[0].LastFiredAt == nil || !tasks[0].LastFiredAt.Equal(fired) {
		got := "<nil>"
		if tasks[0].LastFiredAt != nil {
			got = tasks[0].LastFiredAt.String()
		}
		t.Fatalf("LastFiredAt = %s, want %s", got, fired)
	}
}
