package services_test

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/services"
)

// A state change made through the API must reach live subscribers, or an open
// board stays stale until its fallback poll (T-047).
func TestTaskService_StateChangesPublishTaskUpdated(t *testing.T) {
	ctx := context.Background()
	newSvc := func() (*services.TaskService, *spyTaskBus) {
		store, full := newStore()
		store.getTask = &models.Task{BaseEntity: models.BaseEntity{ID: "task-1", UpdatedAt: time.Now().UTC()}, State: models.TaskStateReady}
		bus := &spyTaskBus{}
		return services.NewTaskService(full, &store.stubBoard).WithBus(bus), bus
	}

	t.Run("UpdateTaskState", func(t *testing.T) {
		svc, bus := newSvc()
		if _, err := svc.UpdateTaskState(ctx, "task-1", models.TaskStateCompleted); err != nil {
			t.Fatalf("UpdateTaskState: %v", err)
		}
		if len(bus.updated) != 1 || bus.updated[0] != "task-1" {
			t.Fatalf("task_updated publishes = %v, want [task-1]", bus.updated)
		}
	})
	t.Run("PatchTask", func(t *testing.T) {
		svc, bus := newSvc()
		next := models.TaskStateCompleted
		if _, err := svc.PatchTask(ctx, "task-1", &next, nil); err != nil {
			t.Fatalf("PatchTask: %v", err)
		}
		if len(bus.updated) != 1 {
			t.Fatalf("task_updated publishes = %v, want 1", bus.updated)
		}
	})
	t.Run("rejected change publishes nothing", func(t *testing.T) {
		svc, bus := newSvc()
		if _, err := svc.UpdateTaskState(ctx, "task-1", models.TaskState("BOGUS")); err == nil {
			t.Fatal("want invalid state error")
		}
		if len(bus.updated) != 0 {
			t.Fatalf("task_updated publishes = %v, want none", bus.updated)
		}
	})
}
