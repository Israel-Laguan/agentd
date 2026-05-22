package safety

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestCircuitBreakerTripsAndHalfOpens(t *testing.T) {
	breaker := NewCircuitBreaker()
	now := time.Now()
	breaker.now = func() time.Time { return now }
	for range 3 {
		breaker.RecordError(models.ErrLLMUnreachable)
	}
	if breaker.State() != BreakerOpen {
		t.Fatalf("state = %s, want OPEN", breaker.State())
	}
	if breaker.FailureCount() != 3 {
		t.Fatalf("failure count = %d, want 3", breaker.FailureCount())
	}
	if !errors.Is(breaker.LastError(), models.ErrLLMUnreachable) {
		t.Fatalf("last error = %v, want ErrLLMUnreachable", breaker.LastError())
	}
	now = now.Add(time.Minute)
	if breaker.OpenDuration() != time.Minute {
		t.Fatalf("open duration = %s, want 1m", breaker.OpenDuration())
	}
	if breaker.AllowRequest() {
		t.Fatal("open breaker allowed request before timeout")
	}
	now = now.Add(defaultBreakerTimeout + time.Second)
	if !breaker.AllowRequest() || breaker.State() != BreakerHalfOpen {
		t.Fatalf("state = %s, want HALF_OPEN allowed", breaker.State())
	}
	breaker.RecordSuccess()
	if breaker.State() != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED", breaker.State())
	}
}

func TestCircuitBreakerIgnoresAgentErrors(t *testing.T) {
	breaker := NewCircuitBreaker()
	for range 3 {
		breaker.RecordError(errors.New("syntax error"))
	}
	if breaker.State() != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED", breaker.State())
	}
}

func TestCircuitBreakerNeverTripsOnNonBreakerErrors(t *testing.T) {
	breaker := NewCircuitBreaker()
	for range 100 {
		breaker.RecordError(errors.New("random failure"))
	}
	if breaker.State() != BreakerClosed {
		t.Fatalf("state = %s after 100 non-breaker errors, want CLOSED", breaker.State())
	}
	if breaker.FailureCount() != 0 {
		t.Fatalf("failure count = %d, want 0 (non-breaker errors not counted)", breaker.FailureCount())
	}
}

func TestSemaphoreCapacity(t *testing.T) {
	sem := NewSemaphore(2)
	ctx := context.Background()
	if !sem.Acquire(ctx) {
		t.Fatal("expected first acquire")
	}
	if !sem.Acquire(ctx) {
		t.Fatal("expected second acquire")
	}
	if sem.Available() != 0 || sem.InUse() != 2 {
		t.Fatalf("available=%d inUse=%d", sem.Available(), sem.InUse())
	}
	sem.Release()
	if sem.Available() != 1 {
		t.Fatalf("available=%d, want 1", sem.Available())
	}
}

func TestSemaphoreAcquireCanceledContext(t *testing.T) {
	sem := NewSemaphore(2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sem.Acquire(ctx) {
		t.Fatal("Acquire on canceled context should return false")
	}
}

func TestCircuitBreakerIsOpen(t *testing.T) {
	breaker := NewCircuitBreaker()
	if breaker.IsOpen() {
		t.Fatal("new breaker should not be open")
	}
	breaker.ForceStateForTest(BreakerOpen, time.Now())
	if !breaker.IsOpen() {
		t.Fatal("expected open after ForceStateForTest")
	}
}

func TestCircuitBreakerNowAndSetClockForTest(t *testing.T) {
	frozen := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker()
	breaker.SetClockForTest(func() time.Time { return frozen })
	if got := breaker.Now(); !got.Equal(frozen) {
		t.Fatalf("Now() = %v, want %v", got, frozen)
	}
}

func TestCircuitBreakerOpenDurationClosed(t *testing.T) {
	breaker := NewCircuitBreaker()
	if d := breaker.OpenDuration(); d != 0 {
		t.Fatalf("OpenDuration on closed breaker = %s, want 0", d)
	}
}

func TestClassifiesAsBreakerFailure(t *testing.T) {
	if !ClassifiesAsBreakerFailure(models.ErrLLMUnreachable) {
		t.Fatal("ErrLLMUnreachable should classify")
	}
	if !ClassifiesAsBreakerFailure(models.ErrLLMQuotaExceeded) {
		t.Fatal("ErrLLMQuotaExceeded should classify")
	}
	if ClassifiesAsBreakerFailure(nil) {
		t.Fatal("nil should not classify")
	}
	if ClassifiesAsBreakerFailure(errors.New("other")) {
		t.Fatal("generic error should not classify")
	}
}

func TestCircuitBreakerArmForResilienceTest(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	trip := now.Add(-3 * time.Minute)
	breaker := NewCircuitBreaker()
	breaker.ArmForResilienceTest(now, trip, 5, models.ErrLLMUnreachable)

	if breaker.State() != BreakerOpen {
		t.Fatalf("state = %s, want OPEN", breaker.State())
	}
	if breaker.FailureCount() != 5 {
		t.Fatalf("failure count = %d, want 5", breaker.FailureCount())
	}
	if !errors.Is(breaker.LastError(), models.ErrLLMUnreachable) {
		t.Fatalf("last error = %v, want ErrLLMUnreachable", breaker.LastError())
	}
	if got := breaker.Now(); !got.Equal(now) {
		t.Fatalf("Now() = %v, want %v", got, now)
	}
	if d := breaker.OpenDuration(); d != 3*time.Minute {
		t.Fatalf("open duration = %s, want 3m", d)
	}
	if breaker.ProbeLimit(10) != 0 {
		t.Fatal("open breaker within timeout should deny probes")
	}
}

func TestCircuitBreakerProbeLimitNonPositive(t *testing.T) {
	breaker := NewCircuitBreaker()
	if got := breaker.ProbeLimit(0); got != 0 {
		t.Fatalf("ProbeLimit(0) = %d, want 0", got)
	}
	if got := breaker.ProbeLimit(-3); got != 0 {
		t.Fatalf("ProbeLimit(-3) = %d, want 0", got)
	}
}

func TestCircuitBreakerProbeLimitOpenBeforeTimeout(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	breaker := NewCircuitBreaker()
	breaker.now = func() time.Time { return now }
	breaker.ForceStateForTest(BreakerOpen, now)
	if got := breaker.ProbeLimit(10); got != 0 {
		t.Fatalf("ProbeLimit while open = %d, want 0", got)
	}
	if breaker.State() != BreakerOpen {
		t.Fatalf("state = %s, want OPEN (no half-open transition yet)", breaker.State())
	}
}

func TestCircuitBreakerProbeLimitHalfOpenInflight(t *testing.T) {
	breaker := NewCircuitBreaker()
	breaker.ForceStateForTest(BreakerHalfOpen, time.Time{})
	if got := breaker.ProbeLimit(5); got != 1 {
		t.Fatalf("first half-open probe = %d, want 1", got)
	}
	if got := breaker.ProbeLimit(5); got != 0 {
		t.Fatalf("second half-open probe while inflight = %d, want 0", got)
	}
	breaker.RecordSuccess()
	if breaker.State() != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED after success", breaker.State())
	}
	if got := breaker.ProbeLimit(5); got != 5 {
		t.Fatalf("closed ProbeLimit = %d, want 5", got)
	}
}

func TestCircuitBreakerForceStateHalfOpenClearsInflight(t *testing.T) {
	breaker := NewCircuitBreaker()
	breaker.ForceStateForTest(BreakerHalfOpen, time.Time{})
	if breaker.ProbeLimit(1) != 1 {
		t.Fatal("expected probe after ForceStateForTest half-open")
	}
	breaker.ForceStateForTest(BreakerHalfOpen, time.Time{})
	if breaker.ProbeLimit(1) != 1 {
		t.Fatal("ForceStateForTest should reset inflight and allow another probe")
	}
}

func TestCircuitBreakerSetClockForTestNilGuards(t *testing.T) {
	var nilBreaker *CircuitBreaker
	nilBreaker.SetClockForTest(func() time.Time { return time.Now() })

	breaker := NewCircuitBreaker()
	before := breaker.Now()
	breaker.SetClockForTest(nil)
	after := breaker.Now()
	if before.IsZero() || after.IsZero() {
		t.Fatal("expected real clock when SetClockForTest(nil)")
	}
}

func TestCircuitBreakerNowNilBreaker(t *testing.T) {
	var breaker *CircuitBreaker
	if got := breaker.Now(); got.IsZero() {
		t.Fatal("nil breaker Now() should return current time")
	}
}

func TestNewSemaphoreMinimumCapacity(t *testing.T) {
	sem := NewSemaphore(0)
	if sem.Capacity() != 1 {
		t.Fatalf("capacity = %d, want 1 for non-positive limit", sem.Capacity())
	}
}

func TestSemaphoreAcquireAtCapacity(t *testing.T) {
	sem := NewSemaphore(2)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if !sem.Acquire(ctx) {
			t.Fatalf("acquire %d failed", i+1)
		}
	}
	if sem.Available() != 0 {
		t.Fatalf("available = %d, want 0 at capacity", sem.Available())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if sem.Acquire(ctx) {
		t.Fatal("Acquire should block then return false when at capacity")
	}
}
