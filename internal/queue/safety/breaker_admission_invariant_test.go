package safety

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agentd/internal/models"
)

// The invariant: AdmissionProbeInFlight is returned only from the HALF_OPEN
// branch of Admit/AdmitFor, so it can never be reported while the breaker is
// CLOSED. A sibling that saw ProbeInFlight requeues to wait for the probe; if a
// CLOSED breaker could report it, healthy siblings would stall behind a probe
// that does not exist.
//
// It is unprovable from outside the package, because a concurrent
// RecordSuccess between Admit's decision and a later State() read looks exactly
// like a violation. So it is proved here, in package safety, from Admit's own
// result.

// closedBreakerWithStaleProbe returns a breaker that is CLOSED *and* still has a
// probe slot taken.
//
// That combination is reachable without any test-only seam, and it is the sharp
// trap for this invariant. RecordSuccessFor sets state to CLOSED and only then
// calls settleProbeLocked, which requires state == BreakerHalfOpen to clear the
// slot — so by the time it looks, the state is already CLOSED and the slot
// survives. Reordering Admit's checks (or reading state before taking the lock)
// turns this breaker into a ProbeInFlight, which is exactly the bug being
// guarded against.
func closedBreakerWithStaleProbe(t *testing.T) *CircuitBreaker {
	t.Helper()
	b := NewCircuitBreaker()
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })
	for range 3 {
		b.RecordError(models.ErrLLMQuotaExceeded)
	}
	if b.State() != BreakerOpen {
		t.Fatalf("setup: state = %s, want OPEN", b.State())
	}
	clock = clock.Add(DefaultBreakerTimeout + time.Second)
	if got := b.Admit(); got != AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want the probe slot", got)
	}
	b.RecordSuccess()

	if b.State() != BreakerClosed {
		t.Fatalf("setup: state = %s, want CLOSED", b.State())
	}
	if !b.inflight {
		t.Fatal("setup: the probe slot was cleared, so this breaker is no longer the trap it is meant to be")
	}
	return b
}

// TestAdmitNeverReportsProbeInFlightWhileClosed is the deterministic half: on a
// breaker that is CLOSED, Admit and AdmitFor must report AdmissionGranted every
// time, no matter what the stale probe slot says.
func TestAdmitNeverReportsProbeInFlightWhileClosed(t *testing.T) {
	b := closedBreakerWithStaleProbe(t)

	for range 100 {
		if got := b.Admit(); got != AdmissionGranted {
			t.Fatalf("Admit() on a CLOSED breaker = %v, want AdmissionGranted — a CLOSED breaker "+
				"takes the first branch and must never be reported as HALF_OPEN with a probe in flight", got)
		}
		if got := b.AdmitFor("sibling"); got != AdmissionGranted {
			t.Fatalf("AdmitFor() on a CLOSED breaker = %v, want AdmissionGranted", got)
		}
	}

	if got := b.State(); got != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED — Admit must not move a CLOSED breaker", got)
	}
	if b.ProbeHeld() {
		t.Fatal("ProbeHeld() = true while CLOSED; no slot is taken while the breaker is closed")
	}
}

// The same trap reached the other way round: a breaker released back to CLOSED
// with its slot still marked must not hand ProbeInFlight to the next caller
// either. ReleaseProbe never changes state, so it cannot be what makes the
// breaker CLOSED — only a recorded success can — which is why this is asserted
// through RecordSuccess above. Here the slot is left over from a HALF_OPEN
// breaker that then had a verdict recorded for a *different* owner (B-014),
// leaving the slot held while the breaker is closed.
func TestAdmitNeverReportsProbeInFlightWhileClosedAfterForeignVerdict(t *testing.T) {
	b := NewCircuitBreaker()
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })
	for range 3 {
		b.RecordErrorFor("tripper", models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(DefaultBreakerTimeout + time.Second)
	if got := b.AdmitFor("probe-holder"); got != AdmissionProbe {
		t.Fatalf("setup: AdmitFor() = %v, want the probe slot", got)
	}
	// A verdict from a task that does not hold the slot closes the breaker but
	// must not settle the slot (B-014), so the breaker ends CLOSED and busy.
	b.RecordSuccessFor("someone-else")
	if b.State() != BreakerClosed || !b.inflight {
		t.Fatalf("setup: state = %s, slot held = %v, want CLOSED with the slot still held", b.State(), b.inflight)
	}

	for range 100 {
		if got := b.AdmitFor("sibling"); got != AdmissionGranted {
			t.Fatalf("AdmitFor() = %v, want AdmissionGranted on a CLOSED breaker with a stale slot", got)
		}
	}
}

// TestAdmitProbeInFlightRequiresAProbeSinceTheLastSuccess is the invariant read
// straight off Admit's result: ProbeInFlight implies a probe was issued after the
// most recent success. A breaker that has been CLOSED since its last
// RecordSuccess has issued none, so it can never report ProbeInFlight.
func TestAdmitProbeInFlightRequiresAProbeSinceTheLastSuccess(t *testing.T) {
	b := NewCircuitBreaker()
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })

	// Closed and never tripped: granted, no slot.
	if got := b.Admit(); got != AdmissionGranted {
		t.Fatalf("Admit() on a fresh breaker = %v, want AdmissionGranted", got)
	}
	if b.ProbeHeld() {
		t.Fatal("a CLOSED breaker took a probe slot")
	}

	// Trip, hold a probe: the holder's sibling sees ProbeInFlight.
	for range 3 {
		b.RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(DefaultBreakerTimeout + time.Second)
	if got := b.Admit(); got != AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want AdmissionProbe", got)
	}
	if got := b.Admit(); got != AdmissionProbeInFlight {
		t.Fatalf("Admit() with the probe held = %v, want AdmissionProbeInFlight", got)
	}

	// The probe resolves: from here on no probe has been issued since the last
	// success, so ProbeInFlight must never be reported again.
	for round := range 20 {
		b.RecordSuccess()
		for range 10 {
			if got := b.Admit(); got == AdmissionProbeInFlight {
				t.Fatalf("round %d: Admit() = AdmissionProbeInFlight, but the breaker has been CLOSED "+
					"by RecordSuccess since the last probe was issued", round)
			}
		}
	}
}

// TestAdmitConcurrentCallersNeverRaceTheStateRead is the concurrent half, and it
// exists to be run under -race.
//
// Sampling the state right after Admit returns would be the wrong check: a
// RecordSuccess from another goroutine can land in between, and the pair then
// looks like a violation when it is not. The meaningful assertion here is not a
// sampling one — it is that reading the state outside the lock is a data race,
// which the detector reports. So this runs every mutator against a swarm of Admit
// callers for a bounded window and asserts only what is race-free: the admission
// count adds up, and the breaker is left in a legal state.
//
// It deliberately does not assert that AdmissionProbeInFlight was observed. An
// earlier version did, and it was flaky under -race for a reason worth recording:
// the verdict writer can finish its whole run before any Admit goroutine is
// scheduled, latching the breaker CLOSED with its slot still marked, after which
// every Admit is AdmissionGranted. In-flight coverage is pinned deterministically
// instead, by TestAdmitProbeInFlightRequiresAProbeSinceTheLastSuccess.
func TestAdmitConcurrentCallersNeverRaceTheStateRead(t *testing.T) {
	b := halfOpenWithHeldProbe(t)
	tally := &admissionTally{}
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// spawn gates every worker behind start and stops them on close(stop), so the
	// window is bounded in time rather than in iterations and all four kinds of
	// mutator overlap the Admit callers for the whole of it.
	start := make(chan struct{})
	spawn := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				select {
				case <-stop:
					return
				default:
					fn()
				}
			}
		}()
	}
	for range 8 {
		spawn(func() { tally.add(b.Admit()) })
	}
	// Cycle the breaker rather than only closing it: three quota failures open it,
	// one success closes it. Cycling keeps the state moving through OPEN and
	// HALF_OPEN while the Admit callers are running.
	spawn(func() {
		for range defaultBreakerFailures {
			b.RecordError(models.ErrLLMQuotaExceeded)
		}
		b.RecordSuccess()
		b.RecordSuccessFor("other")
	})
	spawn(func() {
		b.ReleaseProbe()
		b.ReleaseProbeFor("other")
		b.ProbeHeld()
	})

	close(start)
	time.Sleep(100 * time.Millisecond)
	close(stop)
	wg.Wait()

	if tally.total() == 0 {
		t.Fatal("no Admit completed, so the concurrent case was never exercised")
	}
	switch b.State() {
	case BreakerClosed, BreakerOpen, BreakerHalfOpen:
	default:
		t.Fatalf("breaker left in state %q, want one of CLOSED/OPEN/HALF_OPEN", b.State())
	}
	t.Logf("concurrent admissions: total=%d probe=%d in_flight=%d granted=%d denied=%d",
		tally.total(), tally.probe.Load(), tally.inFlight.Load(), tally.granted.Load(), tally.denied.Load())
}

// halfOpenWithHeldProbe returns a breaker already HALF_OPEN with its probe slot
// taken, so AdmissionProbeInFlight is what a caller meets before any verdict
// lands.
func halfOpenWithHeldProbe(t *testing.T) *CircuitBreaker {
	t.Helper()
	b := NewCircuitBreaker()
	clock := time.Now()
	b.SetClockForTest(func() time.Time { return clock })
	for range 3 {
		b.RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(DefaultBreakerTimeout + time.Second)
	if got := b.Admit(); got != AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want the probe slot held", got)
	}
	return b
}

// admissionTally counts the four Admission outcomes so the soak can assert it
// exercised the in-flight branch and saw a sane total.
type admissionTally struct {
	probe, inFlight, granted, denied atomic.Int64
}

func (a *admissionTally) add(got Admission) {
	switch got {
	case AdmissionProbeInFlight:
		a.inFlight.Add(1)
	case AdmissionProbe:
		a.probe.Add(1)
	case AdmissionGranted:
		a.granted.Add(1)
	default:
		a.denied.Add(1)
	}
}

func (a *admissionTally) total() int64 {
	return a.probe.Load() + a.inFlight.Load() + a.granted.Load() + a.denied.Load()
}
