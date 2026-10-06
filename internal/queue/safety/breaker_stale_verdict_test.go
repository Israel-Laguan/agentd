package safety

import (
	"testing"
	"time"

	"agentd/internal/models"
)

// B-015: a verdict carries no generation, so a success from a request admitted
// before the breaker tripped used to close it. The queue reaches this through
// worker.commitSucceeded: it calls breaker.RecordSuccess() when a task's sandbox
// command succeeds, and that command can run for minutes after the LLM call that
// produced it. A task already executing when three others trip the breaker
// therefore finishes into an OPEN breaker and closes it on evidence that predates
// the outage.
//
// Nothing about OPEN needs a success to leave it: it becomes HALF_OPEN by time
// alone (probeLocked), and the probe's own success closes it. So ignoring a
// success while OPEN cannot strand the breaker, which is the failure B-014 kept
// "any success closes" to avoid.
func trip(b *CircuitBreaker) {
	for i := 0; i < DefaultBreakerFailures; i++ {
		b.RecordError(models.ErrLLMUnreachable)
	}
}

func TestSuccessFromBeforeTheTripDoesNotCloseAnOpenBreaker(t *testing.T) {
	b := NewCircuitBreaker()
	trip(b)
	if b.State() != BreakerOpen {
		t.Fatalf("setup: state = %s, want OPEN", b.State())
	}

	b.RecordSuccess()
	b.RecordSuccessFor("task-admitted-before-the-trip")

	if got := b.State(); got != BreakerOpen {
		t.Fatalf("state = %s, want OPEN: a success that predates the outage closed it", got)
	}
	if got := b.FailureCount(); got != DefaultBreakerFailures {
		t.Fatalf("failure count = %d, want %d: the stale success wiped the evidence of the outage", got, DefaultBreakerFailures)
	}
	if b.LastError() == nil {
		t.Fatal("last error was cleared by a stale success")
	}
}

// The success that is allowed to close an OPEN breaker is the probe's: once the
// timeout passes the breaker is HALF_OPEN, and a success then closes it.
func TestProbeSuccessStillClosesTheBreakerAfterTheTimeout(t *testing.T) {
	b := NewCircuitBreakerWithTimeout(time.Minute)
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })
	trip(b)
	b.RecordSuccess() // stale, ignored while OPEN

	clock = clock.Add(2 * time.Minute)
	if got := b.Admit(); got != AdmissionProbe {
		t.Fatalf("Admit() = %v after the timeout, want AdmissionProbe", got)
	}
	b.RecordSuccess()

	if got := b.State(); got != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED by the probe's success", got)
	}
	if got := b.FailureCount(); got != 0 {
		t.Fatalf("failure count = %d, want 0", got)
	}
}

// What stays accepted, and why it is dropped rather than fixed with a generation
// stamp: a success or failure that is stale in the other places still moves the
// breaker, and each is bounded and self-correcting. A stale success in HALF_OPEN
// closes it, and the next DefaultBreakerFailures failures re-open it, so a wrong
// close costs that many dispatches, never an outage that outlasts them. Stamping
// every admission and threading the stamp through the worker's roll-up and
// handoff paths buys back those dispatches only. If this stops being true the
// drop in B-015 should be revisited.
func TestAStaleCloseCostsAtMostOneTripOfFailures(t *testing.T) {
	b := NewCircuitBreakerWithTimeout(time.Minute)
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })
	trip(b)
	clock = clock.Add(2 * time.Minute)
	if b.Admit() != AdmissionProbe {
		t.Fatal("setup: no probe admitted")
	}

	b.RecordSuccessFor("task-admitted-before-the-trip") // not the probe's, but it still closes in HALF_OPEN
	if b.State() != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED: any success in HALF_OPEN closes (B-014)", b.State())
	}

	for i := 1; i <= DefaultBreakerFailures; i++ {
		if b.State() == BreakerOpen {
			t.Fatalf("re-opened after %d failures, want %d", i-1, DefaultBreakerFailures)
		}
		b.RecordError(models.ErrLLMUnreachable)
	}
	if b.State() != BreakerOpen {
		t.Fatalf("state = %s after %d failures, want OPEN again", b.State(), DefaultBreakerFailures)
	}
}

// The mirror image, also accepted: a failure that is stale because the provider
// has since recovered still counts, but a CLOSED breaker needs
// DefaultBreakerFailures of them in a row to trip and any success clears the
// count, so a lone straggler cannot hold it open.
func TestStaleFailuresBelowTheThresholdDoNotTripAndASuccessClearsThem(t *testing.T) {
	b := NewCircuitBreaker()
	for i := 0; i < DefaultBreakerFailures-1; i++ {
		b.RecordError(models.ErrLLMUnreachable)
	}
	if b.State() != BreakerClosed {
		t.Fatalf("state = %s after %d failures, want CLOSED", b.State(), DefaultBreakerFailures-1)
	}

	b.RecordSuccess()
	b.RecordError(models.ErrLLMUnreachable)

	if b.State() != BreakerClosed || b.FailureCount() != 1 {
		t.Fatalf("state = %s, count = %d, want CLOSED with the count restarted at 1", b.State(), b.FailureCount())
	}
}
