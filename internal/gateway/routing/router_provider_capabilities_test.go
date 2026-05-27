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
		Adapter: "openai",
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
		Adapter: "openai",
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
		Adapter: "ollama",
		BaseURL: "http://localhost:11434",
		Model:   "llama3",
	}, nil))

	if r.ProviderSupportsChatTools("local-ollama") {
		t.Fatal("ProviderSupportsChatTools(local-ollama) = true, want false")
	}
}

func TestRouterProviderSupportsChatTools_EmptyProvider(t *testing.T) {
	t.Parallel()

	t.Run("tool_capable_only", func(t *testing.T) {
		t.Parallel()
		r, err := NewRouterFromConfigs([]spec.ProviderConfig{{
			Name:    "synth-openai",
			Adapter: "openai",
			BaseURL: "https://example.invalid/v1",
			Model:   "test-model",
			APIKey:  "test-key",
		}})
		if err != nil {
			t.Fatalf("NewRouterFromConfigs() error = %v", err)
		}
		if !r.ProviderSupportsChatTools("") {
			t.Fatal("ProviderSupportsChatTools(\"\") = false, want true when a tool-capable provider is configured")
		}
	})

	t.Run("ollama_only", func(t *testing.T) {
		t.Parallel()
		r := NewRouter(providers.NewOllama(spec.ProviderConfig{
			Name:    "ollama",
			Adapter: "ollama",
			BaseURL: "http://localhost:11434",
			Model:   "llama3",
		}, nil))
		if r.ProviderSupportsChatTools("") {
			t.Fatal("ProviderSupportsChatTools(\"\") = true, want false when only ollama is configured")
		}
	})
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
