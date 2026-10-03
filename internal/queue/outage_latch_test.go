package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// T-035: a probe slot spent on a tick that admitted no task must be released,
// otherwise the breaker latches HALF_OPEN and needs an operator reset.
func TestDispatchReleasesProbeSlotWhenQueueIsEmptyAtTimeout(t *testing.T) {
	store := newQueueStore()
	gateway := &queueGateway{content: `{"command":"true"}`}
	sb := &queueSandbox{result: sandbox.Result{Success: true, ExitCode: 0}}
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })
	sink := &queueSink{}
	worker := NewWorker(store, gateway, sb, breaker, sink, WorkerOptions{MaxRetries: DefaultWorkerMaxRetries})
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
