package routing

import (
	"context"
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

func TestRouterProviderSupportsChatTools_CustomNameOpenAIAdapter(t *testing.T) {
	t.Parallel()

	poolside := providers.NewOpenAI(spec.ProviderConfig{
		Name:    "poolside",
		Type:    "openai",
		BaseURL: "https://inference.poolside.ai/v1",
		Model:   "poolside-model",
	}, nil)
	r := NewRouter(poolside)

	if !r.ProviderSupportsChatTools("poolside") {
		t.Fatal("ProviderSupportsChatTools(poolside) = false, want true")
	}
	if r.ProviderSupportsChatTools("openai") {
		t.Fatal("ProviderSupportsChatTools(openai) = true, want false when only poolside is configured")
	}
}

func TestRouterProviderSupportsChatTools_BuiltinName(t *testing.T) {
	t.Parallel()

	r := NewRouter(providers.NewOpenAI(spec.ProviderConfig{
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-4o-mini",
	}, nil))

	if !r.ProviderSupportsChatTools("openai") {
		t.Fatal("ProviderSupportsChatTools(openai) = false, want true")
	}
}

func TestRouterProviderSupportsChatTools_NonToolProvider(t *testing.T) {
	t.Parallel()

	r := NewRouter(providers.NewOllama(spec.ProviderConfig{
		Name:    "local-ollama",
		Type:    "ollama",
		BaseURL: "http://localhost:11434",
		Model:   "llama3",
	}, nil))

	if r.ProviderSupportsChatTools("local-ollama") {
		t.Fatal("ProviderSupportsChatTools(local-ollama) = true, want false")
	}
}

func TestSelectCandidateProviders_CaseInsensitive(t *testing.T) {
	t.Parallel()

	poolside := &mockProvider{
		providerName: "poolside",
		budget:       10000,
		capabilities: providers.Capabilities{SupportsChatTools: true},
	}
	router := NewRouter(poolside).WithTruncation(
		truncation.StrategyTruncator{Strategy: truncation.HeadTailStrategy{HeadRatio: 0.5}},
		12000,
	)

	resp, err := router.Generate(context.Background(), spec.AIRequest{
		Provider: "Poolside",
		Messages: []spec.PromptMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v, want success for case-insensitive provider match", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("content = %q, want ok", resp.Content)
	}
}
