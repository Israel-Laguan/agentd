package kanban

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestUpsertScheduledTaskPreservesLastFiredOnConflict(t *testing.T) {
	store := newTestStore(t)
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

	// Bootstrap-style upsert without LastFiredAt must not clear persisted value.
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

func TestInsertReadyTaskAndRecordDispatchMissingScheduleRollsBack(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	slot := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	_, err = store.InsertReadyTaskAndRecordDispatch(ctx, project.ID, models.DraftTask{
		Title: "Orphan dispatch",
	}, "missing-schedule", slot)
	if err == nil {
		t.Fatal("InsertReadyTaskAndRecordDispatch: want error")
	}
	if !strings.Contains(err.Error(), "missing-schedule") {
		t.Fatalf("error = %v, want schedule id", err)
	}
	tasks, err := store.ListTasks(ctx, models.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.Data) != 0 {
		t.Fatalf("tasks = %d, want 0 (tx rolled back)", len(tasks.Data))
	}
}

// Two ticks that both read the entry as unfired race to dispatch it. The
// dispatch claims the entry, so the loser creates no task: a one-shot fires
// once in all, and a cron entry once per slot.
func TestInsertReadyTaskAndRecordDispatchClaimsTheEntry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	once := time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC)
	for _, e := range []models.ScheduledTask{
		{ID: "once", RunAfter: &once, Kind: models.ScheduledTaskKindDispatch, Enabled: true},
		{ID: "hourly", CronExpr: "0 * * * *", Kind: models.ScheduledTaskKindDispatch, Enabled: true},
	} {
		if err := store.UpsertScheduledTask(ctx, e); err != nil {
			t.Fatalf("UpsertScheduledTask %s: %v", e.ID, err)
		}
	}
	slot := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	dispatch := func(id string, at time.Time) error {
		_, err := store.InsertReadyTaskAndRecordDispatch(ctx, project.ID, models.DraftTask{Title: id}, id, at)
		return err
	}
	steps := []struct {
		id      string
		at      time.Time
		claimed bool
	}{
		{"once", slot, true},
		{"once", slot, false},
		{"once", slot.Add(time.Minute), false},
		{"hourly", slot, true},
		{"hourly", slot, false},
		{"hourly", slot.Add(time.Hour), true},
	}
	for i, s := range steps {
		err := dispatch(s.id, s.at)
		if s.claimed && err != nil {
			t.Fatalf("step %d (%s at %s): %v, want a claim", i, s.id, s.at, err)
		}
		if !s.claimed && !errors.Is(err, models.ErrScheduleAlreadyFired) {
			t.Fatalf("step %d (%s at %s): err = %v, want ErrScheduleAlreadyFired", i, s.id, s.at, err)
		}
	}
	tasks, err := store.ListTasks(ctx, models.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.Data) != 3 {
		t.Fatalf("tasks = %d, want 3 (one-shot once, cron twice)", len(tasks.Data))
	}
}
