package queue

import (
	"context"
	"sync"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// B-017: exactly one task may be handed to a human when three admitted tasks
// fail against an unreachable provider.
//
// The split is part of the contract, not an implementation detail: the failures
// recorded *before* the trip are the outage's, not the task's, so they go back to
// READY with the retry budget untouched, and only the task whose verdict tripped
// the breaker is escalated. HandleGatewayError used to ask that question with two
// separate lock acquisitions — RecordError, then IsOpen — so two tasks that
// recorded 1 and 2 could both be answered with "the breaker is open" once a
// sibling had recorded the third. They were all in flight, so all three were
// escalated and two READY tasks silently became a human inbox.
//
// Each round is a fresh breaker and three tasks released from a barrier, so the
// window between the two calls is hit by scheduling alone.
func TestHandleGatewayErrorHandsOffOnlyTheTrippingTask(t *testing.T) {
	const rounds = 400
	badSplits := 0

	for range rounds {
		store := newQueueStore()
		gateway := &queueGateway{content: `{"command":"true"}`, err: models.ErrLLMUnreachable}
		breaker := NewCircuitBreaker()
		sink := &queueSink{}
		worker := NewWorker(store, gateway, &queueSandbox{result: sandbox.Result{Success: true}},
			breaker, sink, WorkerOptions{MaxRetries: DefaultWorkerMaxRetries})
		store.seed(3, models.TaskStateRunning)

		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, task := range outageStoreTasks(store) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				worker.HandleGatewayError(context.Background(), task, models.ErrLLMUnreachable)
			}()
		}
		close(start)
		wg.Wait()

		ready, blocked := store.count(models.TaskStateReady), store.count(models.TaskStateBlocked)
		if ready != 2 || blocked != 1 {
			badSplits++
		}
	}

	if badSplits != 0 {
		t.Fatalf("%d of %d rounds escalated the wrong number of tasks; want 2 READY and 1 BLOCKED in every round",
			badSplits, rounds)
	}
}

// The trip verdict belongs to the task that recorded it, so the breaker has to
// answer "did my record open it?" under one lock acquisition rather than leaving
// the caller to read the state afterwards.
//
// Sixteen concurrent reports make the property exact rather than statistical: the
// count only ever reaches 16, so exactly the two verdicts recorded before the
// threshold are requeued, whichever callers they happen to belong to, and every
// report from the trip onwards is escalated.
func TestRecordErrorTripsRequeuesOnlyThePreTripVerdicts(t *testing.T) {
	const callers = 16
	breaker := NewCircuitBreaker()
	start := make(chan struct{})
	results := make([]bool, callers)

	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = breaker.RecordErrorTrips(models.ErrLLMUnreachable)
		}()
	}
	close(start)
	wg.Wait()

	requeued := 0
	for _, got := range results {
		if !got {
			requeued++
		}
	}
	if want := DefaultBreakerFailures - 1; requeued != want {
		t.Fatalf("RecordErrorTrips requeued %d of %d verdicts, want %d — only the reports recorded before the trip",
			requeued, callers, want)
	}
	if state := breaker.State(); state != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN", state)
	}
	if got := breaker.FailureCount(); got != callers {
		t.Fatalf("FailureCount = %d, want %d — every report is evidence", got, callers)
	}
}

// A verdict that arrives once the breaker is already OPEN belongs to a task that
// was admitted before the trip, so it is still escalated: requeueing it would
// put a task back in a queue that nothing will dispatch until the timeout.
func TestRecordErrorTripsEscalatesAVerdictRecordedAfterTheTrip(t *testing.T) {
	breaker := NewCircuitBreaker()
	for range DefaultBreakerFailures {
		if breaker.State() != BreakerClosed {
			t.Fatalf("breaker opened after %d failures, want OPEN only from the third",
				breaker.FailureCount())
		}
		tripped := breaker.RecordErrorTrips(models.ErrLLMUnreachable)
		if tripped != (breaker.State() == BreakerOpen) {
			t.Fatalf("RecordErrorTrips = %t at failure %d, breaker state %s",
				tripped, breaker.FailureCount(), breaker.State())
		}
	}
	if !breaker.RecordErrorTrips(models.ErrLLMUnreachable) {
		t.Fatal("a verdict recorded after the trip reported false, want true — the task is still in flight")
	}
	if got := breaker.FailureCount(); got != DefaultBreakerFailures+1 {
		t.Fatalf("FailureCount = %d, want %d", got, DefaultBreakerFailures+1)
	}
}
