package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestUpdateTaskHeartbeatSetsTimestamp(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 1234)
	if running.LastHeartbeat == nil {
		t.Fatal("MarkTaskRunning() did not set initial heartbeat")
	}

	if _, err := store.db.ExecContext(ctx, `UPDATE tasks SET last_heartbeat = NULL WHERE id = ?`, running.ID); err != nil {
		t.Fatalf("clear heartbeat: %v", err)
	}
	if err := store.UpdateTaskHeartbeat(ctx, running.ID); err != nil {
		t.Fatalf("UpdateTaskHeartbeat() error = %v", err)
	}
	updated, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if updated.LastHeartbeat == nil {
		t.Fatal("LastHeartbeat is nil, want timestamp")
	}
}

func TestMarkTaskRunningSetsInitialHeartbeat(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	running := createRunningTask(t, store, ctx, 1234)
	if running.LastHeartbeat == nil {
		t.Fatal("LastHeartbeat is nil, want initial heartbeat")
	}
	if running.State != models.TaskStateRunning {
		t.Fatalf("state = %s, want RUNNING", running.State)
	}
}

func TestReconcileStaleTasksResetsByDeadPID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 4242)

	recovered, err := store.ReconcileStaleTasks(ctx, []int{1111}, 2*time.Minute)
	if err != nil {
		t.Fatalf("ReconcileStaleTasks() error = %v", err)
	}
	if len(recovered) != 1 || recovered[0].ID != running.ID {
		t.Fatalf("recovered = %#v, want task %s", recovered, running.ID)
	}
	assertTaskReadyWithoutHeartbeat(t, store, ctx, running.ID)
}

func TestReconcileStaleTasksResetsByStaleHeartbeat(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 4242)
	staleHeartbeat := time.Now().UTC().Add(-5 * time.Minute)
	if _, err := store.db.ExecContext(ctx, `UPDATE tasks SET last_heartbeat = ? WHERE id = ?`, formatTime(staleHeartbeat), running.ID); err != nil {
		t.Fatalf("set stale heartbeat: %v", err)
	}

	recovered, err := store.ReconcileStaleTasks(ctx, []int{4242}, 2*time.Minute)
	if err != nil {
		t.Fatalf("ReconcileStaleTasks() error = %v", err)
	}
	if len(recovered) != 1 || recovered[0].ID != running.ID {
		t.Fatalf("recovered = %#v, want task %s", recovered, running.ID)
	}
	assertTaskReadyWithoutHeartbeat(t, store, ctx, running.ID)
}

func TestReconcileStaleTasksPreservesHealthyTasks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 4242)

	recovered, err := store.ReconcileStaleTasks(ctx, []int{4242}, 2*time.Minute)
	if err != nil {
		t.Fatalf("ReconcileStaleTasks() error = %v", err)
	}
	if len(recovered) != 0 {
		t.Fatalf("recovered = %#v, want none", recovered)
	}
	healthy, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if healthy.State != models.TaskStateRunning || healthy.OSProcessID == nil || healthy.LastHeartbeat == nil {
		t.Fatalf("healthy task = %#v, want RUNNING with pid and heartbeat", healthy)
	}
}

func createRunningTask(t *testing.T, store *Store, ctx context.Context, pid int) *models.Task {
	t.Helper()
	_, _, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "heartbeat",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, pid)
	if err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	return running
}

func assertTaskReadyWithoutHeartbeat(t *testing.T, store *Store, ctx context.Context, taskID string) {
	t.Helper()
	task, err := store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if task.State != models.TaskStateReady || task.OSProcessID != nil || task.LastHeartbeat != nil {
		t.Fatalf("task = %#v, want READY without pid or heartbeat", task)
	}
}
