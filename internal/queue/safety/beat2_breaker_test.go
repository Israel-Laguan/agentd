package safety

import (
	"errors"
	"testing"

	"agentd/internal/models"
)

// TestBeat2UnreachableOpensBreaker encodes docs/harness-reliability.md Beat 2 Scenario B:
// ErrLLMUnreachable failures trip the breaker OPEN after the threshold.
func TestBeat2UnreachableOpensBreaker(t *testing.T) {
	b := NewCircuitBreaker()
	if !ClassifiesAsBreakerFailure(models.ErrLLMUnreachable) {
		t.Fatal("ErrLLMUnreachable must classify as breaker failure")
	}
	for i := 0; i < 3; i++ {
		b.RecordError(models.ErrLLMUnreachable)
	}
	if b.State() != BreakerOpen {
		t.Fatalf("state = %s, want OPEN after 3 unreachable errors", b.State())
	}
	if !errors.Is(b.LastError(), models.ErrLLMUnreachable) {
		t.Fatalf("last error = %v, want ErrLLMUnreachable", b.LastError())
	}
}

// TestBeat2NonBreakerErrorIgnored ensures unrelated errors do not open the breaker.
func TestBeat2NonBreakerErrorIgnored(t *testing.T) {
	b := NewCircuitBreaker()
	for i := 0; i < 5; i++ {
		b.RecordError(errors.New("sandbox timeout"))
	}
	if b.State() != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED for non-breaker errors", b.State())
	}
}
