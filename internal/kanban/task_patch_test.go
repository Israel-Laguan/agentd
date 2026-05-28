package kanban

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestUpdateTaskPatch_StateAndDescription(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	task := tasksByTitle(tasks)["A"]
	next := models.TaskStateInConsideration
	desc := "patched together"
	updated, err := store.UpdateTaskPatch(ctx, task.ID, task.UpdatedAt, &next, &desc)
	if err != nil {
		t.Fatalf("UpdateTaskPatch: %v", err)
	}
	if updated.State != models.TaskStateInConsideration {
		t.Fatalf("state = %q, want IN_CONSIDERATION", updated.State)
	}
	if updated.Description != desc {
		t.Fatalf("description = %q, want %q", updated.Description, desc)
	}
}

func TestUpdateTaskPatch_InvalidTransitionLeavesRowUnchanged(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	task := tasksByTitle(tasks)["A"]
	origDesc := task.Description

	// READY cannot jump directly to COMPLETED.
	bogus := models.TaskStateCompleted
	_, err = store.UpdateTaskPatch(ctx, task.ID, task.UpdatedAt, &bogus, nil)
	if !errors.Is(err, models.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}

	after, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.State != task.State {
		t.Fatalf("state mutated: got %q, want %q", after.State, task.State)
	}
	if after.Description != origDesc {
		t.Fatalf("description mutated: got %q, want %q", after.Description, origDesc)
	}
}
