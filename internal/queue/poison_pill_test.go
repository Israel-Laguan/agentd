package queue

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestEvictedTaskIsNeverReClaimed(t *testing.T) {
	store := newQueueStore()
	store.seed(1, models.TaskStateReady)

	gw := &queueGateway{content: `{"command":"exit 1"}`}
	sb := &queueSandbox{
		result: sandbox.Result{Success: false, ExitCode: 1},
	}
	breaker := NewCircuitBreaker()
	sink := &queueSink{}
	worker := NewWorker(store, gw, sb, breaker, sink, WorkerOptions{MaxRetries: 3})

	ctx := context.Background()

	for range 3 {
		claimed, err := store.ClaimNextReadyTasks(ctx, 1)
		if err != nil {
			t.Fatalf("ClaimNextReadyTasks() error = %v", err)
		}
		if len(claimed) == 0 {
			break
		}
		worker.Process(ctx, claimed[0])
	}

	if store.count(models.TaskStateFailedRequiresHuman) != 1 {
		t.Fatalf("FAILED_REQUIRES_HUMAN = %d, want 1 (evicted)", store.count(models.TaskStateFailedRequiresHuman))
	}
	if !sink.containsType("POISON_PILL_HANDOFF") {
		t.Fatal("expected POISON_PILL_HANDOFF event")
	}

	for poll := range 10 {
		claimed, err := store.ClaimNextReadyTasks(ctx, 10)
		if err != nil {
			t.Fatalf("poll %d: ClaimNextReadyTasks() error = %v", poll, err)
		}
		for _, task := range claimed {
			if task.ID == "task-0" {
				t.Fatalf("poll %d: evicted task-0 was re-claimed", poll)
			}
		}
	}
}
