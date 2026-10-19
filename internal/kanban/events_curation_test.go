package kanban

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"agentd/internal/models"
)

// completedLongAgo materializes one task and forces it to COMPLETED two days
// ago, so ListCompletedTasksOlderThan(time.Hour) considers it.
func completedLongAgo(t *testing.T, store *Store) models.Task {
	t.Helper()
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "curation",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil || len(tasks) != 1 {
		t.Fatalf("MaterializePlan() = %v, %v", tasks, err)
	}
	old := formatTime(utcNow().Add(-48 * time.Hour))
	if _, err := store.db.ExecContext(ctx,
		`UPDATE tasks SET state = ?, updated_at = ? WHERE id = ?`,
		models.TaskStateCompleted, old, tasks[0].ID); err != nil {
		t.Fatalf("force completed: %v", err)
	}
	return tasks[0]
}

func seedCurationEvent(t *testing.T, store *Store, task models.Task, eventType models.EventType) {
	t.Helper()
	err := store.AppendEvent(context.Background(), models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      eventType,
		Payload:   "p",
	})
	if err != nil {
		t.Fatalf("AppendEvent(%s): %v", eventType, err)
	}
}

func listedIDs(t *testing.T, store *Store) map[string]bool {
	t.Helper()
	tasks, err := store.ListCompletedTasksOlderThan(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("ListCompletedTasksOlderThan: %v", err)
	}
	ids := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		ids[task.ID] = true
	}
	return ids
}

func TestListCompletedTasksOlderThanListsATaskWithEvents(t *testing.T) {
	store := newTestStore(t)
	task := completedLongAgo(t, store)
	seedCurationEvent(t, store, task, models.EventType("LOG"))
	if !listedIDs(t, store)[task.ID] {
		t.Fatal("completed task with events was not listed for curation")
	}
}

func TestListCompletedTasksOlderThanSkipsCuratedTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	task := completedLongAgo(t, store)
	seedCurationEvent(t, store, task, models.EventType("LOG"))
	if err := store.MarkEventsCurated(ctx, task.ID); err != nil {
		t.Fatalf("MarkEventsCurated: %v", err)
	}
	if listedIDs(t, store)[task.ID] {
		t.Fatal("curated task was listed again")
	}
}

// B-066: purging the curated events used to make the task eligible again, so
// every grace period re-ran the summariser (token spend) on the same task.
func TestListCompletedTasksOlderThanSkipsPurgedTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	task := completedLongAgo(t, store)
	seedCurationEvent(t, store, task, models.EventType("LOG"))
	if err := store.MarkEventsCurated(ctx, task.ID); err != nil {
		t.Fatalf("MarkEventsCurated: %v", err)
	}
	if err := store.DeleteCuratedEvents(ctx, task.ID); err != nil {
		t.Fatalf("DeleteCuratedEvents: %v", err)
	}
	seedCurationEvent(t, store, task, models.EventType("EVENTS_PURGED"))
	if listedIDs(t, store)[task.ID] {
		t.Fatal("purged task was listed for curation a second time")
	}
}

// B-066: CurateTask returns early for a task with no events and marks nothing,
// so the listing returned it on every run.
func TestListCompletedTasksOlderThanSkipsTaskWithoutEvents(t *testing.T) {
	store := newTestStore(t)
	task := completedLongAgo(t, store)
	if listedIDs(t, store)[task.ID] {
		t.Fatal("task with zero events was listed for curation")
	}
}
