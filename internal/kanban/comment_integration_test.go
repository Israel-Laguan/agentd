package kanban

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"agentd/internal/models"
)

// registryCanceller adapts a simple map-based cancel registry for testing.
type registryCanceller struct {
	cancels map[string]context.CancelFunc
}

func (r *registryCanceller) Register(taskID string, cancel context.CancelFunc) {
	r.cancels[taskID] = cancel
}

func (r *registryCanceller) Cancel(taskID string) bool {
	cancel, ok := r.cancels[taskID]
	if ok {
		cancel()
	}
	return ok
}

func TestHumanCommentCancelsRunningWorkerContext(t *testing.T) {
	store := newTestStore(t)
	registry := &registryCanceller{cancels: make(map[string]context.CancelFunc)}
	store = store.WithCanceller(registry)

	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "cancel integration",
		Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := store.ClaimNextReadyTasks(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}

	running, err := store.MarkTaskRunning(ctx, claimed[0].ID, claimed[0].UpdatedAt, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	registry.Register(running.ID, cancel)

	err = store.AddComment(ctx, models.Comment{
		TaskID: running.ID,
		Author: "Human",
		Body:   "Stop! Use Python instead of Node",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	select {
	case <-workerCtx.Done():
	case <-time.After(50 * time.Millisecond):
		t.Fatal("worker context was not cancelled within 50ms after human comment")
	}

	_, err = store.UpdateTaskResult(ctx, tasks[0].ID, running.UpdatedAt, models.TaskResult{
		Success: true,
		Payload: "should be rejected",
	})
	if !errors.Is(err, models.ErrStateConflict) {
		t.Fatalf("UpdateTaskResult error = %v, want ErrStateConflict", err)
	}
}
