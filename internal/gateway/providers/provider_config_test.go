package providers

import (
	"testing"

	"agentd/internal/gateway/spec"
)

func TestAppendFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typ  string
		want int
	}{
		{"openai", 1},
		{"anthropic", 1},
		{"ollama", 1},
		{"llamacpp", 1},
		{"horde", 1},
		{"gemini", 1},
		{"unknown", 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.typ, func(t *testing.T) {
			t.Parallel()
			got := AppendFromConfig(nil, spec.ProviderConfig{Type: tt.typ, BaseURL: "http://example"})
			if len(got) != tt.want {
				t.Fatalf("len = %d, want %d", len(got), tt.want)
			}
			if tt.want == 1 && got[0].Name() != spec.Provider(tt.typ) {
				t.Fatalf("Name() = %q, want %q", got[0].Name(), tt.typ)
			}
		})
	}
}

func TestBackendIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		backend    Backend
		provider   spec.Provider
		maxInput   int
	}{
		{
			name:     "openai",
			backend:  NewOpenAI(spec.ProviderConfig{Type: "openai", MaxInputChars: 12000}, nil),
			provider: spec.ProviderOpenAI,
			maxInput: 12000,
		},
		{
			name:     "anthropic",
			backend:  NewAnthropic(spec.ProviderConfig{Type: "anthropic", MaxInputChars: 8000}, nil),
			provider: spec.ProviderAnthropic,
			maxInput: 8000,
		},
		{
			name:     "ollama",
			backend:  NewOllama(spec.ProviderConfig{Type: "ollama", MaxInputChars: 4000}, nil),
			provider: spec.ProviderOllama,
			maxInput: 4000,
		},
		{
			name:     "llamacpp",
			backend:  NewLlamaCpp(spec.ProviderConfig{Type: "llamacpp", MaxInputChars: 3000}, nil),
			provider: spec.ProviderLlamaCpp,
			maxInput: 3000,
		},
		{
			name:     "horde",
			backend:  NewHorde(spec.ProviderConfig{Type: "horde", MaxInputChars: 2000}, nil),
			provider: spec.ProviderHorde,
			maxInput: 2000,
		},
		{
			// 16000 is the configured gateway truncation budget (MaxInputChars → Router.applyTruncation), not a Gemini API character limit.
			name:     "gemini",
			backend:  NewOpenAI(spec.ProviderConfig{Type: "gemini", MaxInputChars: 16000}, nil),
			provider: spec.ProviderGemini,
			maxInput: 16000,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.backend.Name(); got != tt.provider {
				t.Fatalf("Name() = %q, want %q", got, tt.provider)
			}
			if got := tt.backend.MaxInputChars(); got != tt.maxInput {
				t.Fatalf("MaxInputChars() = %d, want %d", got, tt.maxInput)
			}
		})
	}
}
