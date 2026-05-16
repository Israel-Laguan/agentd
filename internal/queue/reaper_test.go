package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestReaperCancelsWorkerAfterDeadline(t *testing.T) {
	store := newQueueStore()
	store.seed(1, models.TaskStateReady)

	sb := &queueSandbox{blockOnCtx: true, started: make(chan struct{}), cancelled: make(chan struct{})}
	gw := &queueGateway{content: `{"command":"sleep 999"}`}
	breaker := NewCircuitBreaker()
	sink := &queueSink{}
	worker := NewWorker(store, gw, sb, breaker, sink, WorkerOptions{})

	deadline := 200 * time.Millisecond
	daemon := NewDaemon(store, worker, nil, breaker, sink, DaemonOptions{
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
	case <-sb.started:
	case <-time.After(2 * time.Second):
		t.Fatal("sandbox did not start")
	}

	select {
	case <-sb.cancelled:
	case <-time.After(deadline + 2*time.Second):
		t.Fatal("sandbox was not cancelled by reaper deadline")
	}

	waitSemaphore(t, daemon.sem, 1, 5*time.Second)
}

func TestReaperDoesNotCancelFastWorker(t *testing.T) {
	store := newQueueStore()
	store.seed(1, models.TaskStateReady)

	gw := &queueGateway{content: `{"command":"true"}`}
	sb := &queueSandbox{result: sandbox.Result{Success: true, ExitCode: 0}}
	breaker := NewCircuitBreaker()
	sink := &queueSink{}
	worker := NewWorker(store, gw, sb, breaker, sink, WorkerOptions{})

	daemon := NewDaemon(store, worker, nil, breaker, sink, DaemonOptions{
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
