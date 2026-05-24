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

type backendCapabilityCase struct {
	name          string
	cfg           spec.ProviderConfig
	wantChatTools bool
}

var backendCapabilityCases = []backendCapabilityCase{
	{
		name:          "openai",
		cfg:           spec.ProviderConfig{Name: "openai", Type: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o"},
		wantChatTools: true,
	},
	{
		name:          "anthropic",
		cfg:           spec.ProviderConfig{Name: "anthropic", Type: "anthropic", BaseURL: "https://api.anthropic.com", Model: "claude-3-5-haiku-latest"},
		wantChatTools: true,
	},
	{
		name:          "gemini",
		cfg:           spec.ProviderConfig{Name: "gemini", Type: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", Model: "gemini-2.5-flash"},
		wantChatTools: true,
	},
	{
		name:          "ollama",
		cfg:           spec.ProviderConfig{Name: "ollama", Type: "ollama", BaseURL: "http://localhost:11434", Model: "llama3"},
		wantChatTools: false,
	},
	{
		name:          "llamacpp",
		cfg:           spec.ProviderConfig{Name: "llamacpp", Type: "llamacpp", BaseURL: "http://localhost:8080", Model: "local"},
		wantChatTools: false,
	},
	{
		name:          "horde",
		cfg:           spec.ProviderConfig{Name: "horde", Type: "horde", BaseURL: "https://stablehorde.net/api/v2", Model: "aphrodite"},
		wantChatTools: false,
	},
}

// TestRouterBackendCapabilityConsistency verifies that for every standard adapter type,
// ProviderSupportsChatTools(name) agrees with Backend.Capabilities().SupportsChatTools.
// This locks the single-source-of-truth invariant so adapter drift is caught early.
func TestRouterBackendCapabilityConsistency(t *testing.T) {
	t.Parallel()

	for _, tc := range backendCapabilityCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertBackendCapabilityConsistency(t, tc)
		})
	}
}

func assertBackendCapabilityConsistency(t *testing.T, tc backendCapabilityCase) {
	t.Helper()

	// Build a backend directly to get its declared capability.
	var backendList []providers.Backend
	backendList, err := providers.AppendFromConfig(backendList, tc.cfg)
	if err != nil {
		t.Fatalf("AppendFromConfig(%q): %v", tc.name, err)
	}
	if len(backendList) != 1 {
		t.Fatalf("AppendFromConfig returned %d backends, want 1", len(backendList))
	}
	backendCap := backendList[0].Capabilities().SupportsChatTools

	// Build a router from the same config and query via ProviderSupportsChatTools.
	r, err := NewRouterFromConfigs([]spec.ProviderConfig{tc.cfg})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs(%q): %v", tc.name, err)
	}
	routerCap := r.ProviderSupportsChatTools(tc.name)

	if backendCap != routerCap {
		t.Errorf("capability mismatch for %q: backend.Capabilities().SupportsChatTools=%v, router.ProviderSupportsChatTools=%v",
			tc.name, backendCap, routerCap)
	}
	if routerCap != tc.wantChatTools {
		t.Errorf("ProviderSupportsChatTools(%q) = %v, want %v", tc.name, routerCap, tc.wantChatTools)
	}
}
