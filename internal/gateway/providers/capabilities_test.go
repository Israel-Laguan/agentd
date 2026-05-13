package providers

import (
	"testing"

	"agentd/internal/gateway/spec"
)

func TestProviderCapabilitiesMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		backend           Backend
		supportsChatTools bool
	}{
		{
			name:              "openai",
			backend:           NewOpenAI(spec.ProviderConfig{BaseURL: "https://api.openai.com/v1", Model: "gpt-test"}, nil),
			supportsChatTools: true,
		},
		{
			name:              "anthropic",
			backend:           NewAnthropic(spec.ProviderConfig{BaseURL: "https://api.anthropic.com", Model: "claude-test"}, nil),
			supportsChatTools: true,
		},
		{
			name:              "ollama",
			backend:           NewOllama(spec.ProviderConfig{BaseURL: "http://localhost:11434", Model: "llama-test"}, nil),
			supportsChatTools: false,
		},
		{
			name:              "llamacpp",
			backend:           NewLlamaCpp(spec.ProviderConfig{BaseURL: "http://localhost:8080", Model: "llama-test"}, nil),
			supportsChatTools: false,
		},
		{
			name:              "horde",
			backend:           NewHorde(spec.ProviderConfig{BaseURL: "https://aihorde.net/api", Model: "horde-test"}, nil),
			supportsChatTools: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			caps := tt.backend.Capabilities()
			if caps.SupportsChatTools != tt.supportsChatTools {
				t.Fatalf("SupportsChatTools = %v, want %v", caps.SupportsChatTools, tt.supportsChatTools)
			}
		})
	}
}
