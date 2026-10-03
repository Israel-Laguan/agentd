package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// A dispatched global HALF_OPEN probe must give its slot back when the task ends
// without recording a breaker verdict.
//
// The gate happens before dispatch: ProbeLimit takes the slot, then the task runs.
// Most completion paths never touch the breaker — a non-zero exit, a requeue, a
// breakdown roll-up, FailHard, a recovered panic — and the slot stays held. Every
// later ProbeLimit then returns 0 and dispatch stops until an operator resets the
// breaker by hand. The provider gate in the worker has a deferred release for the
// same reason (worker_provider_gate.go); this is the global breaker's equivalent.
func TestDispatchReleasesGlobalProbeWhenProbeTaskFailsWithoutVerdict(t *testing.T) {
	store := newQueueStore()
	gateway := &queueGateway{content: `{"command":"false"}`}
	// A non-zero exit: the gateway answered, so nothing here is a breaker failure.
	sb := &queueSandbox{result: sandbox.Result{Success: false, ExitCode: 1, Stdout: "boom"}}
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })
	sink := &queueSink{}
	worker := NewWorker(store, gateway, sb, breaker, sink, WorkerOptions{MaxRetries: DefaultWorkerMaxRetries})
	daemon := NewDaemon(store, worker, nil, breaker, sink, DaemonOptions{MaxWorkers: 3, TaskInterval: time.Hour})

	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	if breaker.State() != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN", breaker.State())
	}
	now = now.Add(DefaultBreakerTimeout + time.Second)

	store.seed(1, models.TaskStateReady)

	ctx := context.Background()
	dispatched, _, err := daemon.dispatch(ctx)
	if err != nil {
		t.Fatalf("dispatch() error = %v", err)
	}
	if dispatched != 1 {
		t.Fatalf("dispatched = %d, want 1 (the breaker admits exactly one probe)", dispatched)
	}
	if err := waitFor(func() bool { return daemon.sem.InUse() == 0 }, "probe task finished"); err != nil {
		t.Fatalf("probe task did not finish: %v", err)
	}

	if breaker.State() != BreakerHalfOpen {
		t.Fatalf("breaker state = %s, want HALF_OPEN — a non-breaker exit must not close or reopen it", breaker.State())
	}
	if breaker.ProbeHeld() {
		t.Fatal("probe slot still held after the probe task finished without a verdict; later ticks cannot dispatch")
	}
	if got := breaker.ProbeLimit(1); got != 1 {
		t.Fatalf("ProbeLimit after the probe task = %d, want 1 — the slot must be available to the next probe", got)
	}
}

// Releasing the slot must not overwrite an outcome the task did record: a probe
// that fails against the provider reopens the breaker and restarts the timeout.
func TestDispatchReleasesGlobalProbeWithoutLosingARecordedOutcome(t *testing.T) {
	breaker := NewCircuitBreaker()
	now := time.Now().UTC()
	breaker.SetClockForTest(func() time.Time { return now })

	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	now = now.Add(DefaultBreakerTimeout + time.Second)
	if got := breaker.ProbeLimit(1); got != 1 {
		t.Fatalf("ProbeLimit = %d, want 1", got)
	}

	breaker.RecordError(models.ErrLLMUnreachable)
	daemon := NewDaemon(newQueueStore(), nil, nil, breaker, nil, DaemonOptions{})
	daemon.releaseGlobalProbe(true)

	if breaker.State() != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN — the release must preserve the recorded outcome", breaker.State())
	}
	now = now.Add(DefaultBreakerTimeout - time.Second)
	if got := breaker.ProbeLimit(1); got != 0 {
		t.Fatalf("ProbeLimit %s after the failed probe = %d, want 0 — the timeout restarts",
			DefaultBreakerTimeout-time.Second, got)
	}
}
