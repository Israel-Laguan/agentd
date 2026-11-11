package queue

import (
	"testing"
	"time"

	"agentd/internal/models"
)

// The dispatch-side provider gate these tests covered (the probe slot, the
// sibling wait, the handoff while a provider is open) went with the builtin exec
// path in T-070. A task that reaches exec.turn now runs on an external agent,
// agentd makes no provider request of its own for it, and there is nothing to
// admit. The registry itself is still fed by the gateway's own quota errors and
// is pinned where it is read: internal/gateway's router tests for the sentinel,
// and outage_trip_handoff_test.go for HandleGatewayError.

func TestProviderBreakersHonourConfiguredTimeout(t *testing.T) {
	pb := NewProviderBreakersWithTimeout(10 * time.Second)
	clock := time.Now()
	pb.Get("gemini").SetClockForTest(func() time.Time { return clock })
	for i := 0; i < 3; i++ {
		pb.Get("gemini").RecordError(models.ErrLLMQuotaExceeded)
	}
	clock = clock.Add(11 * time.Second)
	if got := pb.Get("gemini").Admit(); got != AdmissionProbe {
		t.Fatalf("Admit() after 11s of a 10s timeout = %v, want the probe slot", got)
	}
}
