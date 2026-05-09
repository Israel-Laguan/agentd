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
