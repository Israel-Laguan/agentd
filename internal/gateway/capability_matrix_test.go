package gateway

import (
	"testing"

	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/spec"
)

// adapterCapabilityCase defines the expected capabilities for a single adapter type
// when constructed with a synthetic (non-vendor) provider name.  Adding a new
// OpenAI-compatible vendor via config does NOT require a new row here — new entries
// only appear when a brand-new adapter type (new wire protocol) is introduced.
var adapterCapabilityCase = []struct {
	adapterType   string
	synthName     string
	wantChatTools bool
}{
	{"openai", "synth-openai", true},
	{"anthropic", "synth-anthropic", true},
	{"ollama", "synth-ollama", false},
	{"llamacpp", "synth-llamacpp", false},
	{"horde", "synth-horde", false},
}

func registeredProviderEntries() []spec.ProviderConfig {
	return []spec.ProviderConfig{
		{Name: "synth-openai", Adapter: "openai", BaseURL: "http://example.invalid"},
		{Name: "synth-anthropic", Adapter: "anthropic", BaseURL: "http://example.invalid"},
		{Name: "synth-ollama", Adapter: "ollama", BaseURL: "http://example.invalid"},
		{Name: "synth-llamacpp", Adapter: "llamacpp", BaseURL: "http://example.invalid"},
		{Name: "synth-horde", Adapter: "horde", BaseURL: "http://example.invalid"},
		// Synthetic OpenAI-compatible vendor name.
		{Name: "synth-openai-compat", Adapter: "openai", BaseURL: "http://example.invalid"},
	}
}

// TestCapabilityMatrix_BackendDirect verifies that each adapter type returns the
// expected Capabilities() value regardless of the provider name assigned to it.
// This is the primary guard against adapter-level regressions.
func TestCapabilityMatrix_BackendDirect(t *testing.T) {
	t.Parallel()

	for _, tc := range adapterCapabilityCase {
		tc := tc
		t.Run(tc.adapterType, func(t *testing.T) {
			t.Parallel()
			backends, err := providers.AppendFromConfig(nil, spec.ProviderConfig{
				Name:    tc.synthName,
				Adapter: tc.adapterType,
				BaseURL: "http://example.invalid",
			})
			if err != nil {
				t.Fatalf("AppendFromConfig(%q) error = %v", tc.adapterType, err)
			}
			if len(backends) != 1 {
				t.Fatalf("AppendFromConfig(%q) returned %d backends, want 1", tc.adapterType, len(backends))
			}
			got := backends[0].Capabilities().SupportsChatTools
			if got != tc.wantChatTools {
				t.Errorf("adapter %q: Capabilities().SupportsChatTools = %v, want %v", tc.adapterType, got, tc.wantChatTools)
			}
		})
	}
}

// TestCapabilityMatrix_RouterConsistency verifies that Router.ProviderSupportsChatTools
// agrees with Backend.Capabilities().SupportsChatTools for every adapter type.  This
// catches a Task 10-style bug where a router string-switch would diverge from the
// backend's own capability declaration.
func TestCapabilityMatrix_RouterConsistency(t *testing.T) {
	t.Parallel()

	for _, tc := range adapterCapabilityCase {
		tc := tc
		t.Run(tc.adapterType, func(t *testing.T) {
			t.Parallel()
			cfg := spec.ProviderConfig{
				Name:    tc.synthName,
				Adapter: tc.adapterType,
				BaseURL: "http://example.invalid",
			}
			router, err := NewRouterFromConfigs([]spec.ProviderConfig{cfg})
			if err != nil {
				t.Fatalf("NewRouterFromConfigs(%q) error = %v", tc.adapterType, err)
			}

			// router.ProviderSupportsChatTools must agree with the expected value …
			if got := router.ProviderSupportsChatTools(tc.synthName); got != tc.wantChatTools {
				t.Errorf("router.ProviderSupportsChatTools(%q) = %v, want %v", tc.synthName, got, tc.wantChatTools)
			}
			// … and with what the backend reports directly.
			backends, err := providers.AppendFromConfig(nil, cfg)
			if err != nil {
				t.Fatalf("AppendFromConfig(%q) error = %v", tc.adapterType, err)
			}
			backendCaps := backends[0].Capabilities().SupportsChatTools
			if router.ProviderSupportsChatTools(tc.synthName) != backendCaps {
				t.Errorf("router and backend disagree for adapter %q: router=%v backend=%v",
					tc.adapterType,
					router.ProviderSupportsChatTools(tc.synthName),
					backendCaps,
				)
			}
		})
	}
}

// TestCapabilityMatrix_RegisteredEntriesConsistency verifies the core invariant:
// for every registered provider entry, Router.ProviderSupportsChatTools(name) must
// match backend.Capabilities().SupportsChatTools for that same entry.
func TestCapabilityMatrix_RegisteredEntriesConsistency(t *testing.T) {
	t.Parallel()

	entries := registeredProviderEntries()
	router, err := NewRouterFromConfigs(entries)
	if err != nil {
		t.Fatalf("NewRouterFromConfigs() error = %v", err)
	}

	for _, entry := range entries {
		entry := entry
		t.Run(entry.Name, func(t *testing.T) {
			t.Parallel()

			backends, err := providers.AppendFromConfig(nil, entry)
			if err != nil {
				t.Fatalf("AppendFromConfig(%q) error = %v", entry.Name, err)
			}
			if len(backends) != 1 {
				t.Fatalf("AppendFromConfig(%q) returned %d backends, want 1", entry.Name, len(backends))
			}

			backendCaps := backends[0].Capabilities().SupportsChatTools
			if got := router.ProviderSupportsChatTools(entry.Name); got != backendCaps {
				t.Errorf("ProviderSupportsChatTools(%q) = %v, backend Capabilities().SupportsChatTools = %v",
					entry.Name, got, backendCaps)
			}
		})
	}
}
