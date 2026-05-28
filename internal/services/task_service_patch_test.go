package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/services"
)

func TestTaskService_PatchTask_BothFields(t *testing.T) {
	store, full := newStore()
	now := time.Now().UTC()
	store.getTask = &models.Task{
		BaseEntity:  models.BaseEntity{ID: "task-1", UpdatedAt: now},
		State:       models.TaskStateReady,
		Description: "old",
	}
	svc := services.NewTaskService(full, nil)

	next := models.TaskStateCompleted
	desc := "new desc"
	updated, err := svc.PatchTask(context.Background(), "task-1", &next, &desc)
	if err != nil {
		t.Fatalf("PatchTask: %v", err)
	}
	if updated.State != models.TaskStateCompleted {
		t.Fatalf("state = %q, want COMPLETED", updated.State)
	}
	if updated.Description != "new desc" {
		t.Fatalf("description = %q, want new desc", updated.Description)
	}
}

func TestTaskService_PatchTask_InvalidState(t *testing.T) {
	_, full := newStore()
	svc := services.NewTaskService(full, nil)

	bogus := models.TaskState("BOGUS")
	if _, err := svc.PatchTask(context.Background(), "task-1", &bogus, nil); !errors.Is(err, models.ErrInvalidStateTransition) {
		t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
	}
}

func TestTaskService_PatchTask_TaskNotFound(t *testing.T) {
	store, full := newStore()
	store.getTask = nil
	svc := services.NewTaskService(full, nil)

	next := models.TaskStateCompleted
	if _, err := svc.PatchTask(context.Background(), "missing", &next, nil); !errors.Is(err, models.ErrTaskNotFound) {
		t.Fatalf("expected ErrTaskNotFound, got %v", err)
	}
}
