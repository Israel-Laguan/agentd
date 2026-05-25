package worker

import (
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// newWorkerWithProviders builds a Worker whose gateway is a Router containing the
// given provider configs. Tests that need providerSupportsAgentic to resolve names
// must call this instead of constructing a nil-gateway Worker.
func newWorkerWithProviders(t *testing.T, cfgs ...spec.ProviderConfig) *Worker {
	t.Helper()
	gw, err := gateway.NewRouterFromConfigs(cfgs)
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}
	return &Worker{gateway: gw}
}

// toolCapableConfig returns a minimal ProviderConfig for a chat-tool-capable provider.
func toolCapableConfig(name, providerType string) spec.ProviderConfig {
	return spec.ProviderConfig{Name: name, Adapter: providerType, BaseURL: "https://example.com/v1", Model: "test-model", APIKey: "test-key"}
}

// noToolConfig returns a minimal ProviderConfig for an Ollama provider (no tool support).
func noToolConfig(name string) spec.ProviderConfig {
	return spec.ProviderConfig{Name: name, Adapter: "ollama", BaseURL: "http://localhost:11434", Model: "llama-test"}
}

// TestProviderSupportsAgentic_RouterBacked verifies providerSupportsAgentic via the
// router for all built-in provider types. This replaces the old nil-gateway tests
// and confirms that the router is the single source of truth for chat-tool capability.
func TestProviderSupportsAgentic_RouterBacked(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		providerCfg spec.ProviderConfig
		query       string
		want        bool
	}{
		// OpenAI adapter providers — all return true
		{"openai lowercase", toolCapableConfig("openai", "openai"), "openai", true},
		{"openai uppercase", toolCapableConfig("openai", "openai"), "OPENAI", true},
		{"anthropic lowercase", toolCapableConfig("anthropic", "anthropic"), "anthropic", true},
		{"anthropic uppercase", toolCapableConfig("anthropic", "anthropic"), "ANTHROPIC", true},
		// Gemini reuses the OpenAI adapter; must reach the agentic loop.
		{"gemini lowercase", toolCapableConfig("gemini", "gemini"), "gemini", true},
		{"gemini uppercase", toolCapableConfig("gemini", "gemini"), "GEMINI", true},
		// Ollama intentionally has no chat-tool support.
		{"ollama lowercase", noToolConfig("ollama"), "ollama", false},
		{"ollama uppercase", noToolConfig("ollama"), "OLLAMA", false},
		// LlamaCpp and Horde also have no chat-tool support.
		{"llamacpp", spec.ProviderConfig{Name: "llamacpp", Adapter: "llamacpp", BaseURL: "http://localhost:8080", Model: "local"}, "llamacpp", false},
		{"horde", spec.ProviderConfig{Name: "horde", Adapter: "horde", BaseURL: "https://stablehorde.net/api/v2", Model: "aphrodite"}, "horde", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorkerWithProviders(t, tc.providerCfg)
			profile := models.AgentProfile{ID: "test", Provider: tc.query, Model: "any"}
			if got := w.providerSupportsAgentic(profile); got != tc.want {
				t.Errorf("providerSupportsAgentic(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

// TestProviderSupportsAgentic_UnknownProvider verifies that a provider not present
// in the router's config returns false (router is strict, no fallback guessing).
func TestProviderSupportsAgentic_UnknownProvider(t *testing.T) {
	t.Parallel()

	unknowns := []string{"azure-openai", "vertex", "unknown"}
	w := newWorkerWithProviders(t, toolCapableConfig("openai", "openai"))

	for _, name := range unknowns {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			profile := models.AgentProfile{ID: "test", Provider: name, Model: "any"}
			if w.providerSupportsAgentic(profile) {
				t.Errorf("providerSupportsAgentic(%q) = true, want false (not in router)", name)
			}
		})
	}
}

// TestProviderSupportsAgentic_NilGateway verifies that a Worker with no gateway
// configured returns false for all providers (safe default).
func TestProviderSupportsAgentic_NilGateway(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	for _, name := range []string{"openai", "anthropic", "gemini", "ollama", ""} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			profile := models.AgentProfile{ID: "test", Provider: name, Model: "any"}
			if w.providerSupportsAgentic(profile) {
				t.Errorf("providerSupportsAgentic(%q) = true, want false (nil gateway)", name)
			}
		})
	}
}

func TestProviderSupportsAgentic_CustomProviderName(t *testing.T) {
	t.Parallel()

	w := newWorkerWithProviders(t, spec.ProviderConfig{
		Name:    "poolside",
		Adapter: "openai",
		BaseURL: "https://inference.poolside.ai/v1",
		Model:   "poolside-model",
		APIKey:  "test-key",
	})
	profile := models.AgentProfile{ID: "test", Provider: "poolside", Model: "poolside-model"}
	if !w.providerSupportsAgentic(profile) {
		t.Fatal("providerSupportsAgentic(poolside) = false, want true")
	}
}

// TestProviderSupportsAgentic_EmptyProviderUsesCascade verifies seeded/PATCHed profiles
// with empty provider delegate agentic capability to gateway.order (Task 13).
func TestProviderSupportsAgentic_EmptyProviderUsesCascade(t *testing.T) {
	t.Parallel()

	w := newWorkerWithProviders(t, spec.ProviderConfig{
		Name:    "gemini",
		Adapter: "gemini",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Model:   "gemini-2.5-flash",
		APIKey:  "test-key",
	})
	profile := models.AgentProfile{ID: "default", Provider: "", Model: ""}
	if !w.providerSupportsAgentic(profile) {
		t.Fatal("providerSupportsAgentic(\"\") = false with gemini router, want true (cascade)")
	}
}

// TestProviderSupportsAgentic_GeminiReachesAgenticLoop is the canonical regression
// guard for Task 10: a Worker with a gemini-configured router must NOT fall back to
// legacy mode.
func TestProviderSupportsAgentic_GeminiReachesAgenticLoop(t *testing.T) {
	t.Parallel()

	w := newWorkerWithProviders(t, spec.ProviderConfig{
		Name:    "gemini",
		Adapter: "gemini",
		BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai",
		Model:   "gemini-2.5-flash",
		APIKey:  "test-key",
	})
	profile := models.AgentProfile{ID: "agent-gemini", Provider: "gemini", Model: "gemini-2.5-flash"}
	if !w.providerSupportsAgentic(profile) {
		t.Fatal("providerSupportsAgentic(gemini) = false — Gemini would silently fall back to " +
			"legacy mode; this is the Task 10 regression")
	}
}

// TestProviderSupportsAgentic_OllamaRemainsLegacy is the intentional-false regression
// guard: Ollama must continue to fall back to legacy one-shot JSON.
func TestProviderSupportsAgentic_OllamaRemainsLegacy(t *testing.T) {
	t.Parallel()

	w := newWorkerWithProviders(t, noToolConfig("ollama"))
	profile := models.AgentProfile{ID: "agent-ollama", Provider: "ollama", Model: "llama3:8b"}
	if w.providerSupportsAgentic(profile) {
		t.Fatal("providerSupportsAgentic(ollama) = true — Ollama agentic mode is intentionally disabled")
	}
}

// TestProviderSupportsAgentic_ImportFromGateway verifies that the provider constant
// is correctly imported from gateway package.
func TestProviderSupportsAgentic_ImportFromGateway(t *testing.T) {
	t.Parallel()
	if string(gateway.ProviderOpenAI) != "openai" {
		t.Errorf("gateway.ProviderOpenAI = %q, want \"openai\"", gateway.ProviderOpenAI)
	}
}
