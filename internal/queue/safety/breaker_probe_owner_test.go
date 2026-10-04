package safety

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"agentd/internal/models"
)

// halfOpenAfterTimeout returns a breaker that has tripped and whose open timeout
// has elapsed, so the next admission takes the HALF_OPEN probe slot. Callers
// advance the returned clock to move past a timeout that restarts.
func halfOpenAfterTimeout(t *testing.T) (*CircuitBreaker, *time.Time) {
	t.Helper()
	b := NewCircuitBreaker()
	now := time.Now()
	b.SetClockForTest(func() time.Time { return now })
	for range defaultBreakerFailures {
		b.RecordError(models.ErrLLMUnreachable)
	}
	now = now.Add(DefaultBreakerTimeout + time.Second)
	return b, &now
}

// B-014: task A holds the probe slot. A task admitted while the breaker was CLOSED
// and still running records an error. That verdict must not hand task B a second
// probe slot, or two gateway calls reach a provider believed down.
func TestForeignRecordErrorDoesNotFreeProbeSlot(t *testing.T) {
	b, _ := halfOpenProbeHeld(t, "task-a")

	b.RecordErrorFor("task-b", models.ErrLLMUnreachable)

	if got := b.AdmitFor("task-c"); got != AdmissionProbeInFlight {
		t.Fatalf("AdmitFor(task-c) after an unrelated task's error = %v, want the probe slot still held by task-a", got)
	}
	if b.FailureCount() != 1 {
		t.Fatalf("FailureCount = %d, want 1 — a foreign verdict still counts", b.FailureCount())
	}
}

// A foreign verdict is allowed to change the breaker state; only the slot is off
// limits. A foreign trip must not latch the slot: the next outage gets a probe.
func TestForeignRecordErrorStillTripsTheBreaker(t *testing.T) {
	b, clock := halfOpenAfterTimeout(t)
	b.AdmitFor("task-a")
	b.RecordErrorFor("task-a", models.ErrLLMUnreachable) // the probe fails; the breaker is OPEN
	if got := b.State(); got != BreakerOpen {
		t.Fatalf("setup: state = %s, want OPEN", got)
	}
	*clock = clock.Add(DefaultBreakerTimeout + time.Second)
	b.AdmitFor("task-a") // the next outage's probe, still held by task-a

	b.RecordErrorFor("task-b", models.ErrLLMUnreachable)
	b.RecordErrorFor("task-b", models.ErrLLMUnreachable)
	b.RecordErrorFor("task-b", models.ErrLLMUnreachable)

	if got := b.State(); got != BreakerOpen {
		t.Fatalf("state after foreign failures = %s, want OPEN", got)
	}
	if got := b.AdmitFor("task-d"); got != AdmissionDenied {
		t.Fatalf("AdmitFor(task-d) = %v, want denied while the restarted timeout runs", got)
	}
	*clock = clock.Add(DefaultBreakerTimeout + time.Second)
	if got := b.AdmitFor("task-d"); got != AdmissionProbe {
		t.Fatalf("AdmitFor(task-d) at the restarted timeout = %v, want a probe", got)
	}
}

// A foreign success still closes the breaker — a real gateway answer is evidence
// the provider is up — but it leaves no stale slot behind when the next outage's
// timeout runs.
func TestForeignRecordSuccessClosesWithoutLeavingAStaleSlot(t *testing.T) {
	b, clock := halfOpenAfterTimeout(t)
	if got := b.AdmitFor("task-a"); got != AdmissionProbe {
		t.Fatalf("setup: AdmitFor(task-a) = %v, want the probe slot", got)
	}

	b.RecordSuccessFor("task-b")

	if got := b.State(); got != BreakerClosed {
		t.Fatalf("state after a foreign success = %s, want CLOSED", got)
	}
	for range defaultBreakerFailures {
		b.RecordErrorFor("task-b", models.ErrLLMUnreachable)
	}
	if got := b.ProbeLimit(1); got != 0 {
		t.Fatalf("ProbeLimit while OPEN = %d, want 0", got)
	}
	*clock = clock.Add(DefaultBreakerTimeout + time.Second)
	if got := b.ProbeLimit(1); got != 1 {
		t.Fatalf("ProbeLimit after the timeout = %d, want 1 — a foreign success must not latch the next probe", got)
	}
	if got := b.AdmitFor("task-d"); got != AdmissionProbeInFlight {
		t.Fatalf("AdmitFor(task-d) = %v, want probe in flight — that slot is taken", got)
	}
}

// The other half of the contract: the holder's own outcome does settle the probe.
func TestProbeHolderVerdictSettlesTheSlot(t *testing.T) {
	t.Run("error reopens and restarts the timeout", func(t *testing.T) {
		b, _ := halfOpenAfterTimeout(t)
		if got := b.AdmitFor("task-a"); got != AdmissionProbe {
			t.Fatalf("setup: AdmitFor(task-a) = %v, want the probe slot", got)
		}
		b.RecordErrorFor("task-a", models.ErrLLMUnreachable)
		if got := b.State(); got != BreakerOpen {
			t.Fatalf("state = %s, want OPEN after a failed probe", got)
		}
		if got := b.AdmitFor("task-b"); got != AdmissionDenied {
			t.Fatalf("AdmitFor(task-b) = %v, want denied — the timeout must restart", got)
		}
	})
	t.Run("success closes and frees the slot", func(t *testing.T) {
		b, _ := halfOpenAfterTimeout(t)
		b.AdmitFor("task-a")
		b.RecordSuccessFor("task-a")
		if got := b.State(); got != BreakerClosed {
			t.Fatalf("state = %s, want CLOSED after a successful probe", got)
		}
		if got := b.AdmitFor("task-b"); got != AdmissionGranted {
			t.Fatalf("AdmitFor(task-b) = %v, want granted", got)
		}
	})
}

// A failed probe whose failure count is still below the trip threshold leaves the
// breaker HALF_OPEN, so nothing else changes: the holder's own outcome must still
// settle the slot. If it did not, the breaker would sit HALF_OPEN with the slot
// taken forever — no task can probe, no timeout can restart, and only an operator
// reset frees it. The same latch is what the no-verdict release exists to avoid.
func TestFailedProbeBelowTheTripThresholdStillFreesTheSlot(t *testing.T) {
	t.Run("the next task may probe", func(t *testing.T) {
		b, _ := halfOpenProbeHeld(t, "task-a")
		b.RecordErrorFor("task-a", models.ErrLLMUnreachable)
		if got := b.State(); got != BreakerHalfOpen {
			t.Fatalf("state = %s, want HALF_OPEN — one failure is under the trip threshold", got)
		}
		if got := b.AdmitFor("task-b"); got != AdmissionProbe {
			t.Fatalf("AdmitFor(task-b) = %v, want the probe slot — a failed probe must not latch it", got)
		}
	})
	t.Run("an unowned slot is still settled by a plain verdict", func(t *testing.T) {
		b, _ := halfOpenProbeHeld(t, "task-a")
		b.RecordErrorFor("task-a", models.ErrLLMUnreachable)
		// The dispatch path admits on behalf of no task, so its slot carries no
		// owner and the plain verdict must settle it.
		if got := b.ProbeLimit(1); got != 1 {
			t.Fatalf("ProbeLimit = %d, want 1", got)
		}
		if !b.ProbeHeld() {
			t.Fatal("ProbeHeld() = false right after ProbeLimit, want the slot taken")
		}
		b.RecordError(models.ErrLLMUnreachable)
		if b.ProbeHeld() {
			t.Fatal("ProbeHeld() = true after the plain verdict, want the unowned slot settled")
		}
	})
}

// The record path mirrors the release path: a task-less verdict, like a task-less
// release, must not settle a slot some task is holding. A task-less *success* is
// the documented exception — it still closes the breaker, which is the existing
// behaviour and the safer failure — but a closed breaker takes no slot, so the
// stale one is unobservable here and cannot latch the next outage (see
// TestForeignRecordSuccessClosesWithoutLeavingAStaleSlot).
func TestUnscopedVerdictDoesNotSettleAScopedSlot(t *testing.T) {
	b, _ := halfOpenProbeHeld(t, "task-a")

	b.RecordError(models.ErrLLMUnreachable)

	if got := b.AdmitFor("task-b"); got != AdmissionProbeInFlight {
		t.Fatalf("AdmitFor(task-b) after a plain verdict = %v, want the slot still held by task-a", got)
	}
	if b.FailureCount() != 1 {
		t.Fatalf("FailureCount = %d, want 1 — the plain verdict still counts", b.FailureCount())
	}
	b.RecordSuccess()
	if got := b.State(); got != BreakerClosed {
		t.Fatalf("state after a plain success = %s, want CLOSED", got)
	}
	if b.ProbeHeld() {
		t.Fatal("ProbeHeld() = true while CLOSED, want no slot taken")
	}
}

// A probe that ends with no verdict must give the slot back, and only the holder
// may do so: a task-less release must not steal a scoped slot either.
func TestReleaseProbeForHonoursTheHolder(t *testing.T) {
	b, _ := halfOpenAfterTimeout(t)
	if got := b.AdmitFor("task-a"); got != AdmissionProbe {
		t.Fatalf("setup: AdmitFor(task-a) = %v, want the probe slot", got)
	}

	b.ReleaseProbeFor("task-b")
	if got := b.AdmitFor("task-c"); got != AdmissionProbeInFlight {
		t.Fatalf("AdmitFor(task-c) after a foreign release = %v, want the slot still held by task-a", got)
	}
	b.ReleaseProbe()
	if got := b.AdmitFor("task-c"); got != AdmissionProbeInFlight {
		t.Fatalf("AdmitFor(task-c) after a task-less release = %v, want a scoped slot to resist it", got)
	}
	b.ReleaseProbeFor("task-a")
	if got := b.AdmitFor("task-c"); got != AdmissionProbe {
		t.Fatalf("AdmitFor(task-c) after the holder released = %v, want the probe slot", got)
	}
}

// The global dispatch slot is held on behalf of no task, so the plain unscoped
// API must keep settling it exactly as it did before the slot was scoped.
func TestUnscopedVerdictSettlesTheUnownedSlot(t *testing.T) {
	t.Run("ProbeLimit then RecordError", func(t *testing.T) {
		b, _ := halfOpenAfterTimeout(t)
		if got := b.ProbeLimit(1); got != 1 {
			t.Fatalf("ProbeLimit = %d, want 1", got)
		}
		b.RecordError(models.ErrLLMUnreachable)
		if got := b.ProbeLimit(1); got != 0 {
			t.Fatalf("ProbeLimit after the probe task failed = %d, want 0 — the timeout must restart", got)
		}
	})
	t.Run("Admit then RecordSuccess", func(t *testing.T) {
		b, _ := halfOpenAfterTimeout(t)
		if got := b.Admit(); got != AdmissionProbe {
			t.Fatalf("Admit() = %v, want the probe slot", got)
		}
		b.RecordSuccess()
		if got := b.State(); got != BreakerClosed {
			t.Fatalf("state = %s, want CLOSED", got)
		}
	})
	t.Run("unscoped release frees an unscoped slot", func(t *testing.T) {
		b, _ := halfOpenAfterTimeout(t)
		b.Admit()
		b.ReleaseProbe()
		if got := b.Admit(); got != AdmissionProbe {
			t.Fatalf("Admit() after ReleaseProbe() = %v, want the probe slot back", got)
		}
	})
}

// halfOpenWithCleanFailureCount returns a breaker sitting in the exact window
// B-014 is about: HALF_OPEN with the probe slot taken and no failure counted yet,
// so a foreign verdict raises the count without tripping the breaker — the only
// state in which freeing the slot is observable.
func halfOpenProbeHeld(t *testing.T, owner string) (*CircuitBreaker, *time.Time) {
	t.Helper()
	b := NewCircuitBreaker()
	now := time.Now()
	b.SetClockForTest(func() time.Time { return now })
	b.ArmForResilienceTest(now, now, 0, nil)
	now = now.Add(DefaultBreakerTimeout + time.Second)
	b.SetClockForTest(func() time.Time { return now }) // re-pin: arming froze the clock
	if got := b.AdmitFor(owner); got != AdmissionProbe {
		t.Fatalf("setup: AdmitFor(%s) = %v, want the probe slot", owner, got)
	}
	return b, &now
}

// pollBehindProbe queues one sibling behind a probe it must not get, and reports
// whether it was handed one anyway. It returns once the breaker closes (an
// ordinary request) or it runs out of attempts.
func pollBehindProbe(b *CircuitBreaker, owner string, mu *sync.Mutex, outstanding, granted *int, t *testing.T) {
	for range 64 {
		switch b.AdmitFor(owner) {
		case AdmissionProbe:
			mu.Lock()
			*granted++
			*outstanding++
			over := *outstanding > 1
			mu.Unlock()
			if over {
				t.Errorf("more than one outstanding probe: %d", *outstanding)
			}
			b.RecordSuccessFor(owner)
			mu.Lock()
			*outstanding--
			mu.Unlock()
			return
		case AdmissionGranted:
			return // the breaker closed: an ordinary request, not a probe
		case AdmissionDenied, AdmissionProbeInFlight:
		}
	}
}

// N siblings race a HALF_OPEN breaker whose probe is held by task-a, while an
// unrelated goroutine keeps recording verdicts for a task that was admitted while
// the breaker was CLOSED. task-a never settles, so at no point may a second probe
// be outstanding — a foreign verdict that freed the slot would hand one to a
// sibling.
func TestConcurrentVerdictsNeverGrantTwoProbes(t *testing.T) {
	const siblings = 24
	b, _ := halfOpenProbeHeld(t, "task-a")

	// The verdict that caused B-014, from a task that is not the probe.
	b.RecordErrorFor("unrelated", models.ErrLLMUnreachable)

	var mu sync.Mutex
	outstanding, grantedToSibling := 1, 0

	// The unrelated task keeps reporting while the siblings queue up behind the
	// probe. Only one more verdict: the one above already counted 1, and the
	// breaker trips at 3 — going to OPEN would leave the siblings seeing only
	// AdmissionDenied, so they would never exercise the probe-in-flight path
	// this test is about. Under -race the noise still contends on the breaker
	// lock alongside the 24 sibling goroutines.
	var noise sync.WaitGroup
	noise.Add(1)
	go func() {
		defer noise.Done()
		b.RecordErrorFor("unrelated", models.ErrLLMUnreachable)
	}()

	var wg sync.WaitGroup
	for i := range siblings {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pollBehindProbe(b, "sibling-"+strconv.Itoa(i), &mu, &outstanding, &grantedToSibling, t)
		}(i)
	}
	wg.Wait()
	noise.Wait()

	mu.Lock()
	defer mu.Unlock()
	if grantedToSibling != 0 {
		t.Fatalf("%d siblings were granted a probe while task-a still held it, want 0", grantedToSibling)
	}
	if outstanding != 1 {
		t.Fatalf("outstanding probes = %d, want 1 — task-a's", outstanding)
	}
	if b.FailureCount() == 0 {
		t.Fatal("FailureCount = 0, want the unrelated task's verdicts to have counted")
	}
	// Once the holder reports, the breaker is settled and siblings run normally.
	b.RecordErrorFor("task-a", models.ErrLLMUnreachable)
	if got := b.AdmitFor("task-a"); got != AdmissionDenied {
		t.Fatalf("AdmitFor after the holder's failed probe = %v, want denied — the timeout restarts", got)
	}
}
