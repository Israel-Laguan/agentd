package providers

import (
	"testing"

	"agentd/internal/gateway/spec"
)

// Default adapter capabilities are asserted by Test*AdapterContract in
// adapter_contract_test.go. This file covers config-driven capability overrides only.

func boolPtr(v bool) *bool { return &v }

func TestProviderCapabilitiesConfigOverride(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend Backend
		want    bool
	}{
		{
			name: "ollama enable via config",
			backend: NewOllama(spec.ProviderConfig{
				BaseURL:      "http://localhost:11434",
				Model:        "llama-test",
				Capabilities: spec.ProviderCapabilities{ChatTools: boolPtr(true)},
			}, nil),
			want: true,
		},
		{
			name: "openai disable via config",
			backend: NewOpenAI(spec.ProviderConfig{
				BaseURL:      "https://api.openai.com/v1",
				Model:        "gpt-test",
				Capabilities: spec.ProviderCapabilities{ChatTools: boolPtr(false)},
			}, nil),
			want: false,
		},
		{
			name: "anthropic disable via config",
			backend: NewAnthropic(spec.ProviderConfig{
				BaseURL:      "https://api.anthropic.com",
				Model:        "claude-test",
				Capabilities: spec.ProviderCapabilities{ChatTools: boolPtr(false)},
			}, nil),
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.backend.Capabilities().SupportsChatTools; got != tt.want {
				t.Fatalf("SupportsChatTools = %v, want %v", got, tt.want)
			}
		})
	}
}
