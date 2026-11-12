package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
)

// The reaper cancels a task that outruns its deadline. What it cancels now is an
// agent: the worker's run context reaches the agent process, which the runtime's
// cancellation hook sees.
func TestReaperCancelsWorkerAfterDeadline(t *testing.T) {
	store := newQueueStore()
	store.seed(1, models.TaskStateReady)
	store.tasks[0].Title = fakeagent.Script(fakeagent.Step{SleepMS: 60_000})
	started, stopped := make(chan struct{}), make(chan struct{})
	acp, closeACP := newObservedACPRuntime(started, stopped)
	t.Cleanup(closeACP)
	sink := &queueSink{}
	worker := NewWorker(store, &fakeGateway{}, nil, NewCircuitBreaker(), sink,
		WorkerOptions{Routes: acpQueueRoutes(), ACP: acp})

	deadline := 200 * time.Millisecond
	daemon := NewDaemon(store, worker, nil, NewCircuitBreaker(), sink, DaemonOptions{
		MaxWorkers: 1, TaskInterval: time.Hour, TaskDeadline: deadline,
		Probe: StaticPIDProbe{},
	})

	dispatched, _, err := daemon.dispatch(context.Background())
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1", dispatched)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the agent did not start")
	}

	select {
	case <-stopped:
	case <-time.After(deadline + 2*time.Second):
		t.Fatal("the agent process was not cancelled by the reaper deadline")
	}

	waitSemaphore(t, daemon.sem, 1, 5*time.Second)
}

func TestReaperDoesNotCancelFastWorker(t *testing.T) {
	store := newQueueStore()
	store.seed(1, models.TaskStateReady)
	store.tasks[0].Title = fakeagent.Script(acpReport("done"))
	acp, closeACP := newACPRuntime()
	t.Cleanup(closeACP)
	sink := &queueSink{}
	worker := NewWorker(store, &fakeGateway{}, nil, NewCircuitBreaker(), sink,
		WorkerOptions{Routes: acpQueueRoutes(), ACP: acp})

	daemon := NewDaemon(store, worker, nil, NewCircuitBreaker(), sink, DaemonOptions{
		MaxWorkers: 1, TaskInterval: time.Hour, TaskDeadline: time.Minute,
		Probe: StaticPIDProbe{},
	})

	dispatched, _, err := daemon.dispatch(context.Background())
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1", dispatched)
	}

	waitSemaphore(t, daemon.sem, 1, 5*time.Second)

	if store.count(models.TaskStateCompleted) != 1 {
		t.Fatalf("completed = %d, want 1", store.count(models.TaskStateCompleted))
	}
}
