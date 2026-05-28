package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestUpdateCriteriaMetPersistsToRunningTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 9001)

	met := []string{"file exists", "tests pass"}
	if err := store.UpdateCriteriaMet(ctx, running.ID, met); err != nil {
		t.Fatalf("UpdateCriteriaMet() error = %v", err)
	}

	got, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(got.CriteriaMet) != 2 {
		t.Fatalf("CriteriaMet len = %d, want 2", len(got.CriteriaMet))
	}
	if got.CriteriaMet[0] != "file exists" || got.CriteriaMet[1] != "tests pass" {
		t.Fatalf("CriteriaMet = %v, want [file exists tests pass]", got.CriteriaMet)
	}
}

func TestUpdateCriteriaMetNoOpForNonRunningTask(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 9002)

	// Complete the task so it's no longer RUNNING.
	if _, err := store.UpdateTaskResult(ctx, running.ID, running.UpdatedAt, models.TaskResult{Success: true}); err != nil {
		t.Fatalf("UpdateTaskResult() error = %v", err)
	}

	// UpdateCriteriaMet should silently no-op.
	if err := store.UpdateCriteriaMet(ctx, running.ID, []string{"should be ignored"}); err != nil {
		t.Fatalf("UpdateCriteriaMet() on non-running task error = %v, want nil", err)
	}

	got, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if len(got.CriteriaMet) != 0 {
		t.Fatalf("CriteriaMet = %v, want empty (task was not RUNNING)", got.CriteriaMet)
	}
}

func TestUpdateCriteriaMetEmptySlice(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	running := createRunningTask(t, store, ctx, 9003)

	if err := store.UpdateCriteriaMet(ctx, running.ID, nil); err != nil {
		t.Fatalf("UpdateCriteriaMet(nil) error = %v", err)
	}

	got, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if got.CriteriaMet == nil {
		t.Fatal("CriteriaMet is nil, want empty slice")
	}
	if len(got.CriteriaMet) != 0 {
		t.Fatalf("CriteriaMet = %v, want []", got.CriteriaMet)
	}
}
