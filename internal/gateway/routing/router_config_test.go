package routing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
)

func TestNewRouterFromConfigs(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Adapter: "openai", BaseURL: "https://api.openai.com/v1"},
		{Adapter: "anthropic", BaseURL: "https://api.anthropic.com"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}
	if len(router.providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(router.providers))
	}
}

func TestNewRouterFromConfigs_CustomName(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{{Name: "poolside", Adapter: "openai", BaseURL: "https://inference.poolside.ai/v1"}})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}
	if len(router.providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(router.providers))
	}
	if got := router.providers[0].Name(); got != spec.Provider("poolside") {
		t.Fatalf("provider name = %q, want poolside", got)
	}
}

func TestRouterProviderNames(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Name: "poolside", Adapter: "openai", BaseURL: "https://inference.poolside.ai/v1"},
		{Name: "gemini", Adapter: "openai", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}
	got := router.ProviderNames()
	want := []string{"poolside", "gemini"}
	if len(got) != len(want) {
		t.Fatalf("ProviderNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ProviderNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestNewRouterFromConfigs_UnknownAdapter(t *testing.T) {
	t.Parallel()

	_, err := NewRouterFromConfigs([]spec.ProviderConfig{{Adapter: "unknown"}})
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("NewRouterFromConfigs() error = %v, want adapter name in error", err)
	}
}

func TestWithPhaseCap(t *testing.T) {
	t.Parallel()

	router := NewRouter().WithPhaseCap(5).WithPhaseCap(-1)
	if router.maxTasksPerPhase != 5 {
		t.Fatalf("maxTasksPerPhase = %d, want 5", router.maxTasksPerPhase)
	}
}

func TestNewRouterFromConfigs_TwoOpenAIAdapters(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Name: "openai", Adapter: "openai", BaseURL: "https://api.openai.com/v1"},
		{Name: "poolside", Adapter: "openai", BaseURL: "https://inference.poolside.ai/v1"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}
	if len(router.providers) != 2 {
		t.Fatalf("providers = %d, want 2", len(router.providers))
	}
	if got := router.providers[0].Name(); got != spec.Provider("openai") {
		t.Errorf("providers[0].Name() = %q, want openai", got)
	}
	if got := router.providers[1].Name(); got != spec.Provider("poolside") {
		t.Errorf("providers[1].Name() = %q, want poolside", got)
	}
}

func TestRouter_ProviderRouting_PoolsideVsOpenAI(t *testing.T) {
	t.Parallel()

	openai := &mockProvider{providerName: "openai", content: "openai-response",
		capabilities: providers.Capabilities{SupportsChatTools: true}}
	poolside := &mockProvider{providerName: "poolside", content: "poolside-response",
		capabilities: providers.Capabilities{SupportsChatTools: true}}
	router := NewRouter(openai, poolside)

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Messages:  []spec.PromptMessage{{Role: "user", Content: "hi"}},
		Provider: "poolside",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.ProviderUsed != "poolside" {
		t.Errorf("ProviderUsed = %q, want poolside", resp.ProviderUsed)
	}
	if openai.request.Messages != nil {
		t.Errorf("openai backend was called unexpectedly (request = %+v)", openai.request)
	}
}

type stubBudgetTracker struct {
	reserveErr error
	added      int
}

func (b *stubBudgetTracker) Reserve(string) error { return b.reserveErr }
func (b *stubBudgetTracker) Add(string, int)      { b.added++ }
func (b *stubBudgetTracker) Usage(string) int     { return 0 }
func (b *stubBudgetTracker) Reset(string)         {}

func TestRouterReserveBudget(t *testing.T) {
	t.Parallel()

	tracker := &stubBudgetTracker{reserveErr: errors.New("over budget")}
	router := NewRouter(&mockProvider{providerName: "openai", budget: 10000}).
		WithBudget(tracker)

	_, err := router.Generate(context.Background(), spec.AIRequest{
		TaskID:   "task-1",
		Messages: []spec.PromptMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil || !errors.Is(err, tracker.reserveErr) {
		t.Fatalf("Generate() error = %v", err)
	}
}

type embedBackendProvider struct {
	mockProvider
	resp spec.EmbedResponse
	err  error
}

func (p *embedBackendProvider) Embed(context.Context, spec.EmbedRequest) (spec.EmbedResponse, error) {
	if p.err != nil {
		return spec.EmbedResponse{}, p.err
	}
	return p.resp, nil
}

func TestRouterEmbed(t *testing.T) {
	t.Parallel()

	t.Run("no embedding provider", func(t *testing.T) {
		t.Parallel()
		router := NewRouter(&mockProvider{providerName: "ollama", budget: 1000})
		_, err := router.Embed(context.Background(), spec.EmbedRequest{Input: []string{"a"}})
		if err == nil || !strings.Contains(err.Error(), "no embedding provider configured") {
			t.Fatalf("Embed() error = %v", err)
		}
	})

	t.Run("delegates to first embed backend", func(t *testing.T) {
		t.Parallel()
		want := spec.EmbedResponse{ModelUsed: "embed-model", Vectors: [][]float32{{1}}}
		router := NewRouter(&embedBackendProvider{
			mockProvider: mockProvider{providerName: "openai", budget: 1000},
			resp:         want,
		})
		got, err := router.Embed(context.Background(), spec.EmbedRequest{Input: []string{"a"}})
		if err != nil {
			t.Fatalf("Embed() error = %v", err)
		}
		if got.ModelUsed != want.ModelUsed || len(got.Vectors) != 1 {
			t.Fatalf("Embed() = %#v, want %#v", got, want)
		}
	})
}
