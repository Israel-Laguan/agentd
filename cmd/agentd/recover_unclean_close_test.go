package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/kanban"
	"agentd/internal/models"
	"agentd/internal/queue"
)

// TestBootReconcileRecoversRunningTaskAfterUncleanClose tests T-025 Part A: a
// file-backed home with a RUNNING task (dead PID) is reopened while the first
// handle is still open (a killed daemon never calls Close), then BootReconcile
// runs. The task must no longer be RUNNING.
func TestBootReconcileRecoversRunningTaskAfterUncleanClose(t *testing.T) {
	home := initHome(t)
	ctx := context.Background()
	taskID, cleanup := seedRunningTask(t, ctx, home)
	// Deliberately not closed before the reopen: that is the unclean close.
	defer cleanup()
	reopenAndReconcile(t, ctx, home, taskID)
}

func seedRunningTask(t *testing.T, ctx context.Context, home string) (string, func()) {
	t.Helper()
	_, store, _, cleanup, err := openRuntime(&rootOptions{home: home})
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
	return tasks[0].ID, cleanup
}

func reopenAndReconcile(t *testing.T, ctx context.Context, home string, taskID string) {
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
	assertSystemStatusOK(t, store2)
}

// assertSystemStatusOK serves the reopened home through api.NewHandler and
// requires GET /api/v1/system/status to return 200 — the daemon-usable-home
// half of the T-025 Part A contract.
func assertSystemStatusOK(t *testing.T, store *kanban.Store) {
	t.Helper()
	handler := api.NewHandler(api.ServerDeps{
		Store:      store,
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/system/status")
	if err != nil {
		t.Fatalf("GET /api/v1/system/status error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v1/system/status status = %d, want 200", resp.StatusCode)
	}
}
