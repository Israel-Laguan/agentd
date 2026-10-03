package queue

import (
	"context"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// trippedProviderBreakers returns a registry whose "gemini" breaker is OPEN and
// whose clock the test controls.
func trippedProviderBreakers(t *testing.T) (*ProviderBreakers, *time.Time) {
	t.Helper()
	return trippedProviderBreakersFor(t, "gemini")
}

func trippedProviderBreakersFor(t *testing.T, provider string) (*ProviderBreakers, *time.Time) {
	t.Helper()
	pb := NewProviderBreakers()
	clock := time.Now()
	pb.Get(provider).SetClockForTest(func() time.Time { return clock })
	for i := 0; i < 3; i++ {
		pb.Get(provider).RecordError(models.ErrLLMQuotaExceeded)
	}
	if pb.Get(provider).State() != BreakerOpen {
		t.Fatalf("setup: %s breaker state = %s, want OPEN", provider, pb.Get(provider).State())
	}
	return pb, &clock
}

func providerStore() *workerStore {
	store := newWorkerStore()
	store.profile.Provider = "gemini"
	store.profile.AgenticMode = false
	return store
}

// B-012: an OPEN provider breaker must admit a probe once its timeout has passed,
// and a successful probe must close it.
func TestWorkerProviderBreakerProbesAfterTimeout(t *testing.T) {
	store := providerStore()
	pb, clock := trippedProviderBreakers(t)
	*clock = clock.Add(6 * time.Minute)
	gw := &fakeGateway{content: `{"command":"echo ok"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), &recordingSink{}, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if len(gw.requests) == 0 {
		t.Fatalf("gateway requests = 0, want the task dispatched to the provider after the timeout")
	}
	if got := pb.Get("gemini").State(); got != BreakerClosed {
		t.Fatalf("gemini breaker state = %s, want CLOSED after a successful probe", got)
	}
}

func TestWorkerProviderBreakerFailedProbeReopens(t *testing.T) {
	store := providerStore()
	pb, clock := trippedProviderBreakers(t)
	*clock = clock.Add(6 * time.Minute)
	gw := &fakeGateway{err: models.ErrLLMQuotaExceeded}
	worker := NewWorker(store, gw, &fakeSandbox{}, NewCircuitBreaker(), &recordingSink{}, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if got := pb.Get("gemini").State(); got != BreakerOpen {
		t.Fatalf("state after failed probe = %s, want OPEN", got)
	}
	if got := pb.Get("gemini").Admit(); got != AdmissionDenied {
		t.Fatal("a failed probe must restart the timeout, but the breaker admitted again at once")
	}
}

// B-013: while another task holds the probe slot, a sibling for the same provider
// must wait (back to READY, no gateway call, no handoff) rather than be handed to a
// human as if the provider were still down.
func TestWorkerProviderBreakerSiblingWaitsWhileProbeInFlight(t *testing.T) {
	store := providerStore()
	pb, clock := trippedProviderBreakers(t)
	*clock = clock.Add(6 * time.Minute)
	if got := pb.Get("gemini").Admit(); got != AdmissionProbe {
		t.Fatalf("setup: Admit() = %v, want the probe slot", got)
	}
	gw := &fakeGateway{content: `{"command":"echo ok"}`}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, &fakeSandbox{}, NewCircuitBreaker(), sink, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if len(gw.requests) != 0 {
		t.Fatalf("gateway requests = %d, want 0 while another task holds the probe slot", len(gw.requests))
	}
	if store.task.State != models.TaskStateReady {
		t.Fatalf("sibling state = %s, want READY (waiting for the probe)", store.task.State)
	}
	if sink.hasEvent("PROVIDER_EXHAUSTED_HANDOFF") {
		t.Fatal("sibling was handed off while the provider's probe was still in flight")
	}
	if got := pb.Get("gemini").State(); got != BreakerHalfOpen {
		t.Fatalf("breaker state = %s, want HALF_OPEN (the probe has not resolved)", got)
	}
	if sink.hasEvent("RETRY") {
		t.Fatal("waiting on a probe must not emit a RETRY event on every tick")
	}
	if store.task.RetryCount != 0 {
		t.Fatalf("RetryCount = %d, want 0 (waiting is not a retry)", store.task.RetryCount)
	}
}

// A breaker that is genuinely OPEN (timeout not yet reached) still hands the task
// off: only the probe-in-flight case waits.
func TestWorkerProviderBreakerStillOpenHandsOff(t *testing.T) {
	store := providerStore()
	pb, _ := trippedProviderBreakers(t)
	gw := &fakeGateway{content: `{"command":"echo ok"}`}
	sink := &recordingSink{}
	worker := NewWorker(store, gw, &fakeSandbox{}, NewCircuitBreaker(), sink, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if len(gw.requests) != 0 {
		t.Fatalf("gateway requests = %d, want 0 while the provider breaker is OPEN", len(gw.requests))
	}
	if store.task.State == models.TaskStateReady {
		t.Fatalf("task state = %s, want it handed off or failed, not requeued, while OPEN", store.task.State)
	}
}

// A probe that ends without a verdict (here: a non-breaker failure) must hand the
// slot back, or the provider stays HALF_OPEN with no one allowed to probe.
func TestWorkerProviderBreakerReleasesProbeWithoutVerdict(t *testing.T) {
	store := providerStore()
	pb, clock := trippedProviderBreakers(t)
	*clock = clock.Add(6 * time.Minute)
	gw := &fakeGateway{content: `{"command":"false"}`}
	sb := &fakeSandbox{result: sandbox.Result{Success: false, ExitCode: 1}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), &recordingSink{}, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if got := pb.Get("gemini").State(); got != BreakerHalfOpen {
		t.Fatalf("state = %s, want HALF_OPEN (no verdict recorded)", got)
	}
	if got := pb.Get("gemini").Admit(); got != AdmissionProbe {
		t.Fatal("probe slot stayed taken after the probe task ended without a verdict")
	}
}

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

func TestWorkerProviderBreakerClosesAfterAgenticProbe(t *testing.T) {
	store := providerStore()
	store.profile.AgenticMode = true
	store.profile.Provider = "openai"
	pb, clock := trippedProviderBreakersFor(t, "openai")
	*clock = clock.Add(6 * time.Minute)
	gw := &fakeGateway{
		content:     "done",
		toolCalls:   []gateway.ToolCall{{ID: "call_1", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo hello"}`}}},
		nextContent: "done",
	}
	sb := &fakeSandbox{result: sandbox.Result{Success: true, Stdout: "hello"}}
	worker := NewWorker(store, gw, sb, NewCircuitBreaker(), nil, WorkerOptions{ProviderBreakers: pb})

	worker.Process(context.Background(), store.task)

	if store.result == nil || !store.result.Success {
		t.Fatalf("result = %#v, want success", store.result)
	}
	if got := pb.Get("openai").State(); got != BreakerClosed {
		t.Fatalf("state = %s, want CLOSED after a successful agentic probe", got)
	}
}
