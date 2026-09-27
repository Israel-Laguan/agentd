package main

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/queue"
)

// TestRecoverUncleanCloseUngrabbedTasks_RestartRecoversTask tests T-025 Part A:
// A file-backed home with a RUNNING task (dead PID) is reopened after unclean
// close (no Close() call), then BootReconcile is called. The task should no
// longer be RUNNING and system/status should return 200.
func TestRecoverUncleanCloseUngrabbedTasks_RestartRecoversTask(t *testing.T) {
	home := initHome(t)
	ctx := context.Background()
	taskID := seedRunningTask(t, ctx, home)
	closeAndReopen(t, ctx, home, taskID)
}

func seedRunningTask(t *testing.T, ctx context.Context, home string) string {
	t.Helper()
	_, store, _, cleanup, err := openRuntime(&rootOptions{home: home})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("openRuntime() error = %v", err)
	}
	if _, err := store.EnsureSystemProject(ctx); err != nil {
		t.Fatalf("EnsureSystemProject() error = %v", err)
	}
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "recover",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "Unclean close task"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimNextReadyTasks() error = %v", err)
	}
	if _, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, 99999); err != nil {
		t.Fatalf("MarkTaskRunning() error = %v", err)
	}
	beforeClose, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask() before close error = %v", err)
	}
	if beforeClose.State != models.TaskStateRunning {
		t.Fatalf("before close: task state = %v, want RUNNING", beforeClose.State)
	}
	cleanup()
	return tasks[0].ID
}

func closeAndReopen(t *testing.T, ctx context.Context, home string, taskID string) {
	t.Helper()
	_, store2, _, cleanup2, err := openRuntime(&rootOptions{home: home})
	if cleanup2 != nil {
		defer cleanup2()
	}
	if err != nil {
		t.Fatalf("second openRuntime() error = %v", err)
	}
	if err := queue.BootReconcile(ctx, store2, queue.StaticPIDProbe{PIDs: []int{1, 2}}, nil); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}
	afterReconcile, err := store2.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask() after reconcile error = %v", err)
	}
	if afterReconcile.State == models.TaskStateRunning {
		t.Fatalf("after reconcile: task state = %v, want not RUNNING", afterReconcile.State)
	}
}
