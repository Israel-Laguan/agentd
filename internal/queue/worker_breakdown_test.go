package queue

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestWorkerTooComplexWithoutSubtasksFailsNormally(t *testing.T) {
	store := newWorkerStore()
	gw := &fakeGateway{content: `{"too_complex":true}`}
	sb := &fakeSandbox{}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{})

	worker.Process(context.Background(), store.task)

	if store.appends != 0 || store.task.RetryCount != 1 || store.task.State != models.TaskStateReady {
		t.Fatalf("appends=%d retries=%d state=%s", store.appends, store.task.RetryCount, store.task.State)
	}
}
