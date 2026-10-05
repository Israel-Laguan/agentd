package safety

// The HALF_OPEN probe slot: one request at a time may test whether a provider
// believed down has recovered. The slot has an owner, and only that owner settles
// it, so a verdict from any other task updates the failure count and the breaker
// state without handing a second probe to a sibling (B-014). The dispatch path
// takes an unowned slot through ProbeLimit and Admit, for which the plain
// unscoped Record*, ReleaseProbe and ProbeHeld still apply unchanged.

func (b *CircuitBreaker) AllowRequest() bool {
	return b.ProbeLimit(1) > 0
}

func (b *CircuitBreaker) ProbeLimit(available int) int {
	if available <= 0 {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.probeLocked(available)
}

// Admission is the outcome of asking a breaker whether one request may run.
type Admission int

const (
	// AdmissionDenied: the breaker is OPEN and its timeout has not elapsed.
	AdmissionDenied Admission = iota
	// AdmissionProbeInFlight: the breaker is HALF_OPEN and another request holds
	// the probe slot, so the provider's state is not known yet.
	AdmissionProbeInFlight
	// AdmissionGranted: the breaker is CLOSED; no probe slot was taken.
	AdmissionGranted
	// AdmissionProbe: the request took the HALF_OPEN probe slot and must record an
	// outcome or ReleaseProbe.
	AdmissionProbe
)

// Admit is the per-request form of ProbeLimit(1) for callers that gate one
// task at a time. The reason a request is refused is decided under the same lock
// as the refusal, so a probe resolving in between cannot change it.
func (b *CircuitBreaker) Admit() Admission {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == BreakerClosed {
		return AdmissionGranted
	}
	if b.probeLocked(1) > 0 {
		return AdmissionProbe
	}
	if b.state == BreakerHalfOpen {
		return AdmissionProbeInFlight
	}
	return AdmissionDenied
}

// AdmitFor is Admit with the holder recorded, so a later verdict can be told apart
// from another task's. It is AdmitFor("", i.e. Admit) for the dispatch path,
// where the probe belongs to no single task.
func (b *CircuitBreaker) AdmitFor(owner string) Admission {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == BreakerClosed {
		return AdmissionGranted
	}
	if b.probeLocked(1) > 0 {
		b.probeOwner = owner
		return AdmissionProbe
	}
	if b.state == BreakerHalfOpen {
		return AdmissionProbeInFlight
	}
	return AdmissionDenied
}

func (b *CircuitBreaker) probeLocked(available int) int {
	switch b.state {
	case BreakerOpen:
		if b.now().Sub(b.tripTime) < b.timeout {
			return 0
		}
		b.state = BreakerHalfOpen
		b.clearProbeLocked()
		fallthrough
	case BreakerHalfOpen:
		if b.inflight {
			return 0
		}
		b.inflight = true
		// Unowned until the caller says otherwise: ProbeLimit and Admit admit on
		// behalf of no single task, AdmitFor names its owner right after this.
		b.probeOwner = ""
		return 1
	default:
		return available
	}
}

// clearProbeLocked returns the probe slot to the pool and forgets its owner, for
// the transitions that invalidate a probe outright rather than settling it.
func (b *CircuitBreaker) clearProbeLocked() {
	b.inflight = false
	b.probeOwner = ""
}

// ProbeHeld reports whether the HALF_OPEN probe slot is currently taken. A
// caller that asked ProbeLimit for capacity calls it to learn whether it now
// owns a probe it must resolve with an outcome or ReleaseProbe. It is false
// while the breaker is CLOSED, because no slot is taken then.
func (b *CircuitBreaker) ProbeHeld() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == BreakerHalfOpen && b.inflight
}

// ReleaseProbe returns the HALF_OPEN probe slot taken by ProbeLimit when the
// caller ended up dispatching nothing, so a later tick with work can use it.
// It never changes the breaker state: only a recorded outcome does that.
//
// The dispatch path's probe belongs to no single task, so it takes an unowned slot
// and this release matches it. A task-scoped slot has an owner and only that
// owner's release returns it — a task-less release must not free a probe some
// task is still running.
func (b *CircuitBreaker) ReleaseProbe() {
	b.ReleaseProbeFor("")
}

// ReleaseProbeFor returns the HALF_OPEN probe slot when owner took it and the
// probe ended without a verdict, so a later tick with work can use it. It is a
// no-op for any other owner, and never changes the breaker state: only a recorded
// outcome does that.
func (b *CircuitBreaker) ReleaseProbeFor(owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.settleProbeLocked(owner)
}
