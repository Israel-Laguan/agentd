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
	probeOwner   string
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

func (b *CircuitBreaker) RecordError(err error) {
	b.RecordErrorFor("", err)
}

// RecordErrorFor counts a provider failure reported for owner.
//
// The failure count, the last error and the breaker state move whoever reported
// them: a task admitted while the breaker was CLOSED is still evidence about the
// provider, and ignoring it would let a real outage look recovered. Only the
// HALF_OPEN probe slot is reserved for the task that holds it, so a verdict from
// any other task cannot hand a second probe to a sibling (B-014).
func (b *CircuitBreaker) RecordErrorFor(owner string, err error) {
	if !ClassifiesAsBreakerFailure(err) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount++
	b.lastError = err
	b.settleProbeLocked(owner)
	if b.failureCount >= defaultBreakerFailures {
		b.state = BreakerOpen
		b.tripTime = b.now()
	}
}

func (b *CircuitBreaker) RecordSuccess() {
	b.RecordSuccessFor("")
}

// RecordSuccessFor closes the breaker after a provider request for owner
// succeeded.
//
// Any task's success is evidence the provider is up, so the breaker closes
// whoever reports it — that is existing behaviour and a stuck OPEN breaker is the
// worse failure. The probe slot is settled by its holder alone, which is safe
// because probeLocked clears it whenever the breaker next reaches HALF_OPEN.
func (b *CircuitBreaker) RecordSuccessFor(owner string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failureCount = 0
	b.state = BreakerClosed
	b.tripTime = time.Time{}
	b.lastError = nil
	b.settleProbeLocked(owner)
}

// settleProbeLocked returns the HALF_OPEN probe slot to the pool when owner holds
// it, and does nothing for any other owner: only the holder's own outcome settles
// a probe (B-014). The unowned slot taken by ProbeLimit and Admit has no holder
// to match, so the plain unscoped verdict and ReleaseProbe settle it, which is
// what the dispatch path relies on.
func (b *CircuitBreaker) settleProbeLocked(owner string) {
	if b.state == BreakerHalfOpen && b.inflight && b.probeOwner == owner {
		b.inflight = false
	}
}

// ForceStateForTest overrides internal state for integration tests.
func (b *CircuitBreaker) ForceStateForTest(state BreakerState, tripTime time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = state
	b.tripTime = tripTime
	b.clearProbeLocked()
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
	b.clearProbeLocked()
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
	b.clearProbeLocked()
}

func ClassifiesAsBreakerFailure(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, models.ErrLLMUnreachable) || errors.Is(err, models.ErrLLMQuotaExceeded)
}
