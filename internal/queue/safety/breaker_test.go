package safety

import (
	"testing"
	"time"

	"agentd/internal/models"
)

func TestCircuitBreakerReset(t *testing.T) {
	b := NewCircuitBreaker()

	// Trip the breaker by recording 3 quota errors.
	for i := 0; i < 3; i++ {
		b.RecordError(models.ErrLLMQuotaExceeded)
	}
	if b.State() != BreakerOpen {
		t.Fatalf("breaker state = %s, want OPEN", b.State())
	}
	if b.FailureCount() != 3 {
		t.Fatalf("failure count = %d, want 3", b.FailureCount())
	}

	b.Reset()

	if b.State() != BreakerClosed {
		t.Fatalf("after Reset: state = %s, want CLOSED", b.State())
	}
	if b.FailureCount() != 0 {
		t.Fatalf("after Reset: failure count = %d, want 0", b.FailureCount())
	}
	if b.OpenDuration() != 0 {
		t.Fatalf("after Reset: open duration = %v, want 0", b.OpenDuration())
	}
	if b.LastError() != nil {
		t.Fatalf("after Reset: last error = %v, want nil", b.LastError())
	}
	// AllowRequest must work again after reset.
	if !b.AllowRequest() {
		t.Fatal("after Reset: AllowRequest = false, want true")
	}
}

func TestProviderBreakersGetAndRecord(t *testing.T) {
	pb := NewProviderBreakers()

	// Record quota errors on the gemini breaker only.
	for i := 0; i < 3; i++ {
		pb.Get("gemini").RecordError(models.ErrLLMQuotaExceeded)
	}

	if pb.Get("gemini").State() != BreakerOpen {
		t.Fatal("gemini breaker should be OPEN after 3 quota errors")
	}
	// A different provider must be unaffected.
	if pb.Get("horde").State() != BreakerClosed {
		t.Fatal("horde breaker must remain CLOSED")
	}
}

func TestProviderBreakersReset(t *testing.T) {
	pb := NewProviderBreakers()

	for i := 0; i < 3; i++ {
		pb.Get("gemini").RecordError(models.ErrLLMQuotaExceeded)
		pb.Get("openai").RecordError(models.ErrLLMQuotaExceeded)
	}

	// Reset only gemini.
	pb.Reset("gemini")
	if pb.Get("gemini").State() != BreakerClosed {
		t.Fatal("gemini should be CLOSED after Reset")
	}
	if pb.Get("openai").State() != BreakerOpen {
		t.Fatal("openai should still be OPEN (not reset)")
	}

	// ResetAll brings everything back.
	pb.ResetAll()
	if pb.Get("openai").State() != BreakerClosed {
		t.Fatal("openai should be CLOSED after ResetAll")
	}
}

func TestProviderBreakersSnapshot(t *testing.T) {
	pb := NewProviderBreakers()

	now := time.Now()
	pb.Get("gemini").ArmForResilienceTest(now, now.Add(-1*time.Minute), 3, models.ErrLLMQuotaExceeded)

	snap := pb.Snapshot()
	entry, ok := snap["gemini"]
	if !ok {
		t.Fatal("snapshot should contain gemini entry")
	}
	if entry.State != string(BreakerOpen) {
		t.Fatalf("snapshot state = %s, want OPEN", entry.State)
	}
	if entry.LastError == "" {
		t.Fatal("snapshot should carry last error")
	}
}
