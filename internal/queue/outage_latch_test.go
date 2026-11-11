package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
)

// T-035: a probe slot spent on a tick that admitted no task must be released,
// otherwise the breaker latches HALF_OPEN and needs an operator reset. The probe
// task now succeeds on the agent, and a completed task is what closes the
// breaker (commitSucceeded records it), as it did when the task ran in-process.
func TestDispatchReleasesProbeSlotWhenQueueIsEmptyAtTimeout(t *testing.T) {
	store := newQueueStore()
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })
	sink := &queueSink{}
	acp, closeACP := newACPRuntime()
	t.Cleanup(closeACP)
	worker := NewWorker(store, &fakeGateway{}, nil, breaker, sink,
		WorkerOptions{MaxRetries: DefaultWorkerMaxRetries, Routes: acpQueueRoutes(), ACP: acp})
	daemon := NewDaemon(store, worker, nil, breaker, sink, DaemonOptions{MaxWorkers: 3, TaskInterval: time.Hour})

	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	now = now.Add(DefaultBreakerTimeout + time.Second)

	ctx := context.Background()
	if dispatched, _, err := daemon.dispatch(ctx); err != nil || dispatched != 0 {
		t.Fatalf("dispatch() on an empty queue = (%d, %v), want (0, nil)", dispatched, err)
	}

	store.seed(1, models.TaskStateReady)
	store.tasks[0].Title = fakeagent.Script(acpReport("probe ok"))
	dispatched, _, err := daemon.dispatch(ctx)
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d with a READY task after an empty crossing tick, want 1 — "+
			"the probe slot was spent on nothing and the breaker latched %s", dispatched, breaker.State())
	}
	if err := waitFor(func() bool { return daemon.sem.InUse() == 0 }, "probe task finished"); err != nil {
		t.Fatalf("probe task did not finish: %v", err)
	}
	if breaker.State() != BreakerClosed {
		t.Fatalf("breaker state after the probe succeeded = %s, want CLOSED", breaker.State())
	}
}
