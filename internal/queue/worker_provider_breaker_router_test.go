package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// B-016: a provider breaker can only open through the queue path if the error
// that reaches HandleGatewayError still carries the provider errors' identity.
//
// Every test that fed RecordErrorFor a hand-built models.ErrLLMQuotaExceeded
// proved the gate works; none proved the error survives the real cascade. It did
// not: decideTerminalError formatted the joined provider errors with %v, which
// drops the sentinel and leaves errors.Is(err, ErrLLMQuotaExceeded) false, so
// HandleGatewayError took the global-breaker branch instead and no per-provider
// breaker could ever leave CLOSED. These two tests drive the real
// gateway.Router so the error is the one production produces.

// failingProvider is a providers.Backend that always fails with a fixed error, so
// the cascade is exhausted by exactly it and the aggregate is deterministic.
type failingProvider struct {
	name string
	err  error

	mu    sync.Mutex
	calls int
}

func (p *failingProvider) Name() spec.Provider { return spec.Provider(p.name) }
func (p *failingProvider) MaxInputChars() int  { return 100000 }
func (p *failingProvider) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func (p *failingProvider) Generate(context.Context, spec.AIRequest) (spec.AIResponse, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return spec.AIResponse{}, p.err
}

func (p *failingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// processQuotaTasks runs the given number of independent tasks through the real
// router. Each is a fresh id and state, so the worker treats them as separate
// tasks and each gateway round trip is its own verdict — the trip threshold is
// three failures (safety.defaultBreakerFailures).
func processQuotaTasks(t *testing.T, worker *Worker, store *workerStore, count int) {
	t.Helper()

	for i := 0; i < count; i++ {
		store.task.ID = fmt.Sprintf("task-%d", i)
		store.task.State = models.TaskStateQueued
		worker.Process(context.Background(), store.task)
	}
}

// A provider answering 429 must open that provider's own breaker after three
// failures, and hand the task off rather than requeue it. Before the fix the
// quota sentinel was flattened, so every one of these tasks took the global
// breaker's requeue path and the provider breaker stayed CLOSED.
func TestWorkerProviderBreakerOpensFromRealQuotaCascade(t *testing.T) {
	store := providerStore()
	backend := &failingProvider{name: store.profile.Provider, err: models.ErrLLMQuotaExceeded}
	pb := NewProviderBreakers()
	global := NewCircuitBreaker()
	sink := &recordingSink{}
	worker := NewWorker(store, gateway.NewRouter(backend), &fakeSandbox{}, global, sink,
		WorkerOptions{ProviderBreakers: pb})

	processQuotaTasks(t, worker, store, 3)

	if got := pb.Get("gemini").State(); got != BreakerOpen {
		t.Fatalf("gemini breaker state = %s, want OPEN after 3 real quota failures (%d gateway calls)",
			got, backend.callCount())
	}
	if got := pb.Get("gemini").FailureCount(); got != 3 {
		t.Fatalf("gemini failure count = %d, want 3", got)
	}
	if !sink.hasEvent("PROVIDER_EXHAUSTED_HANDOFF") {
		t.Fatal("no PROVIDER_EXHAUSTED_HANDOFF: a quota cascade must hand off, not requeue")
	}
	// The quota branch is provider-scoped (decision (c)): a single provider's
	// exhausted key must not stall unrelated providers through the global breaker.
	if got := global.State(); got != BreakerClosed {
		t.Fatalf("global breaker state = %s, want CLOSED — a provider quota failure is not a global outage", got)
	}
}

// The counterpart, and the guard on decision (c): a genuinely unreachable cascade
// still carries only ErrLLMUnreachable, so it must keep feeding the global
// breaker and must not open a provider breaker for a provider whose quota was
// never involved.
func TestWorkerGlobalBreakerStillFedByRealUnreachableCascade(t *testing.T) {
	store := providerStore()
	backend := &failingProvider{
		name: store.profile.Provider,
		err:  fmt.Errorf("dial tcp: %w", models.ErrLLMUnreachable),
	}
	pb := NewProviderBreakers()
	global := NewCircuitBreaker()
	sink := &recordingSink{}
	worker := NewWorker(store, gateway.NewRouter(backend), &fakeSandbox{}, global, sink,
		WorkerOptions{ProviderBreakers: pb})

	processQuotaTasks(t, worker, store, 3)

	if got := global.State(); got != BreakerOpen {
		t.Fatalf("global breaker state = %s, want OPEN after 3 real unreachable failures (%d gateway calls)",
			got, backend.callCount())
	}
	if got := pb.Get("gemini").State(); got != BreakerClosed {
		t.Fatalf("gemini breaker state = %s, want CLOSED — an outage is not that provider's quota", got)
	}
}

// A cascade of unclassified provider errors is still cascade exhaustion, which
// has always counted as an outage (internal/gateway's
// TestRouterCascadeExhaustion_BreakerRecognizes pins that), so it keeps feeding
// the global breaker. The fix must not change that: only a quota sentinel reaches
// the provider registry, and no provider breaker may open without one.
func TestWorkerUnclassifiedCascadeFeedsOnlyTheGlobalBreaker(t *testing.T) {
	store := providerStore()
	backend := &failingProvider{name: store.profile.Provider, err: errors.New("bad gateway html")}
	pb := NewProviderBreakers()
	global := NewCircuitBreaker()
	worker := NewWorker(store, gateway.NewRouter(backend), &fakeSandbox{}, global, &recordingSink{},
		WorkerOptions{ProviderBreakers: pb})

	processQuotaTasks(t, worker, store, 3)

	if got := global.State(); got != BreakerOpen {
		t.Fatalf("global breaker state = %s, want OPEN — an exhausted cascade is an outage", got)
	}
	if got := pb.Get("gemini").State(); got != BreakerClosed {
		t.Fatalf("gemini breaker state = %s, want CLOSED — no quota sentinel was in the chain", got)
	}
}