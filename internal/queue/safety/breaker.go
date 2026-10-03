package safety

import (
	"errors"
	"sync"
	"time"

	"agentd/internal/models"
)

type BreakerState string

const (
	BreakerClosed   BreakerState = "CLOSED"
	BreakerOpen     BreakerState = "OPEN"
	BreakerHalfOpen BreakerState = "HALF_OPEN"
)

const (
	defaultBreakerFailures = 3
	defaultBreakerTimeout  = 5 * time.Minute
)

// DefaultBreakerTimeout is the built-in half-open wait used by the circuit breaker.
const DefaultBreakerTimeout = defaultBreakerTimeout

type CircuitBreaker struct {
	mu           sync.RWMutex
	state        BreakerState
	failureCount int
	tripTime     time.Time
	lastError    error
	now          func() time.Time
	timeout      time.Duration
	inflight     bool
}

func NewCircuitBreaker() *CircuitBreaker {
	return NewCircuitBreakerWithTimeout(defaultBreakerTimeout)
}

// NewCircuitBreakerWithTimeout returns a breaker that stays OPEN for timeout
// before admitting a probe. A non-positive timeout falls back to the default,
// so a bad config value can never make the pause zero or unbounded.
func NewCircuitBreakerWithTimeout(timeout time.Duration) *CircuitBreaker {
	if timeout <= 0 {
		timeout = defaultBreakerTimeout
	}
	return &CircuitBreaker{state: BreakerClosed, now: time.Now, timeout: timeout}
}

// SetClockForTest replaces the time source (same-package tests may assign .now directly).
func (b *CircuitBreaker) SetClockForTest(now func() time.Time) {
	if b == nil || now == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = now
}

// Now returns the breaker's notion of current time (for outage logging across packages).
func (b *CircuitBreaker) Now() time.Time {
	if b == nil {
		return time.Now()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.now == nil {
		return time.Now()
	}
	return b.now()
}

func (b *CircuitBreaker) State() BreakerState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state
}

func (b *CircuitBreaker) IsOpen() bool { return b.State() == BreakerOpen }

func (b *CircuitBreaker) OpenDuration() time.Duration {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.state != BreakerOpen {
		return 0
	}
	return b.now().Sub(b.tripTime)
}

func (b *CircuitBreaker) FailureCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.failureCount
}

func (b *CircuitBreaker) LastError() error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastError
}

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

func (b *CircuitBreaker) probeLocked(available int) int {
	switch b.state {
	case BreakerOpen:
		if b.now().Sub(b.tripTime) < b.timeout {
			return 0
		}
		b.state = BreakerHalfOpen
		b.inflight = false
		fallthrough
	case BreakerHalfOpen:
		if b.inflight {
			return 0
		}
		b.inflight = true
		return 1
	default:
		return available
	}
}

// ReleaseProbe returns the HALF_OPEN probe slot taken by ProbeLimit when the
// caller ended up dispatching nothing, so a later tick with work can use it.
// It never changes the breaker state: only a recorded outcome does that.
func (b *CircuitBreaker) ReleaseProbe() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == BreakerHalfOpen {
		b.inflight = false
	}
}

func (b *CircuitBreaker) RecordError(err error) {
	if !ClassifiesAsBreakerFailure(err) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount++
	b.lastError = err
	b.inflight = false
	if b.failureCount >= defaultBreakerFailures {
		b.state = BreakerOpen
		b.tripTime = b.now()
	}
}

func (b *CircuitBreaker) RecordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount = 0
	b.state = BreakerClosed
	b.tripTime = time.Time{}
	b.lastError = nil
	b.inflight = false
}

// ForceStateForTest overrides internal state for integration tests.
func (b *CircuitBreaker) ForceStateForTest(state BreakerState, tripTime time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = state
	b.tripTime = tripTime
	b.inflight = false
}

// Reset unconditionally returns the breaker to the CLOSED state and clears
// all failure counters. Intended for operator-initiated recovery via the
// /api/v1/system/breaker/reset endpoint; no probe is required.
func (b *CircuitBreaker) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = BreakerClosed
	b.failureCount = 0
	b.tripTime = time.Time{}
	b.lastError = nil
	b.inflight = false
}

// ArmForResilienceTest configures an OPEN breaker for queue integration tests.
func (b *CircuitBreaker) ArmForResilienceTest(now, tripTime time.Time, failureCount int, lastErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = func() time.Time { return now }
	b.state = BreakerOpen
	b.tripTime = tripTime
	b.failureCount = failureCount
	b.lastError = lastErr
	b.inflight = false
}

func ClassifiesAsBreakerFailure(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, models.ErrLLMUnreachable) || errors.Is(err, models.ErrLLMQuotaExceeded)
}
