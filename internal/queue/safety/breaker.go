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
	return &CircuitBreaker{state: BreakerClosed, now: time.Now, timeout: defaultBreakerTimeout}
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
