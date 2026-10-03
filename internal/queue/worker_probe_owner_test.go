package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// B-014, worker level: this task is admitted while the provider breaker is CLOSED
// and is still running when, mid-request, the breaker trips and a sibling takes
// the HALF_OPEN probe slot. The task's gateway error is a verdict from a task that
// is not the probe, so it must update the failure count and the breaker state but
// leave the probe slot alone — otherwise the next sibling is handed a second
// gateway call at a provider believed down.
func TestWorkerForeignGatewayErrorKeepsTheProbeSlotHeld(t *testing.T) {
	store := providerStore()
	pb := NewProviderBreakers()
	clock := time.Now()
	breaker := pb.Get("gemini")
	breaker.SetClockForTest(func() time.Time { return clock })

	// The task reaches its provider while the breaker is CLOSED.
	if got := breaker.AdmitFor(store.task.ID); got != AdmissionGranted {
		t.Fatalf("setup: AdmitFor(task) = %v, want granted while the breaker is CLOSED", got)
	}

	// Mid-request the breaker trips and a sibling takes the probe slot, with no
	// failure counted yet so this task's own verdict cannot trip it away.
	takeProbe := func() {
		if breaker.State() != BreakerClosed {
			return
		}
		breaker.ArmForResilienceTest(clock, clock, 0, models.ErrLLMQuotaExceeded)
		clock = clock.Add(6 * time.Minute)
		breaker.SetClockForTest(func() time.Time { return clock })
		if got := breaker.AdmitFor("sibling-probe"); got != AdmissionProbe {
			t.Fatalf("setup: AdmitFor(sibling-probe) = %v, want the probe slot", got)
		}
	}

	gw := &fakeGateway{err: models.ErrLLMQuotaExceeded, onRequest: takeProbe}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, &fakeSandbox{result: sandbox.Result{Success: false, ExitCode: 1}}, NewCircuitBreaker(), sink, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if len(gw.requests) == 0 {
		t.Fatal("no provider request, so this test proved nothing about a running task's verdict")
	}
	if store.task.State == models.TaskStateReady {
		t.Fatal("task state = READY, want its own quota error handed off or failed")
	}
	if breaker.FailureCount() == 0 {
		t.Fatal("FailureCount = 0, want this task's gateway error to count against the breaker")
	}
	if !breaker.ProbeHeld() {
		t.Fatal("ProbeHeld() = false after an unrelated task's error, want the sibling's probe still in flight")
	}
	if got := breaker.AdmitFor("late-sibling"); got != AdmissionProbeInFlight {
		t.Fatalf("AdmitFor(late-sibling) = %v, want the probe slot still held by sibling-probe", got)
	}
}
