package routing

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestNewRouterFromConfigs(t *testing.T) {
	t.Parallel()

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{
		{Type: "openai", BaseURL: "https://api.openai.com/v1"},
		{Type: "anthropic", BaseURL: "https://api.anthropic.com"},
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

	router, err := NewRouterFromConfigs([]spec.ProviderConfig{{Name: "poolside", Type: "openai", BaseURL: "https://inference.poolside.ai/v1"}})
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

func TestNewRouterFromConfigs_UnknownAdapter(t *testing.T) {
	t.Parallel()

	_, err := NewRouterFromConfigs([]spec.ProviderConfig{{Type: "unknown"}})
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
