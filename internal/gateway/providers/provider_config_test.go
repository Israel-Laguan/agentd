package providers

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestAppendFromConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		typ     string
		want    int
		wantErr bool
	}{
		{"openai", 1, false},
		{"anthropic", 1, false},
		{"ollama", 1, false},
		{"llamacpp", 1, false},
		{"horde", 1, false},
		{"gemini", 1, false},
		{"unknown", 0, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.typ, func(t *testing.T) {
			t.Parallel()
			got, err := AppendFromConfig(nil, spec.ProviderConfig{Adapter: tt.typ, BaseURL: "http://example"})
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.typ) {
					t.Fatalf("AppendFromConfig() error = %v, want provider name in error", err)
				}
				if len(got) != 0 {
					t.Fatalf("len = %d, want 0 on error", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("AppendFromConfig() error = %v", err)
			}
			if len(got) != tt.want {
				t.Fatalf("len = %d, want %d", len(got), tt.want)
			}
			if tt.want == 1 && got[0].Name() != spec.Provider(tt.typ) {
				t.Fatalf("Name() = %q, want %q", got[0].Name(), tt.typ)
			}
		})
	}
}

func TestAppendFromConfig_CustomName(t *testing.T) {
	t.Parallel()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{Adapter: "openai", Name: "poolside", BaseURL: "http://example"})
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name() != spec.Provider("poolside") {
		t.Fatalf("Name() = %q, want poolside", got[0].Name())
	}
}

// TestAppendFromConfig_AdapterDefault verifies that when Adapter is empty the provider
// Name is used as the adapter selector, preserving backward compatibility.
func TestAppendFromConfig_AdapterDefault(t *testing.T) {
	t.Parallel()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{Name: "openai", BaseURL: "http://example"})
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name() != spec.Provider("openai") {
		t.Fatalf("Name() = %q, want openai", got[0].Name())
	}
}

var backendIdentityCases = []struct {
	name     string
	backend  Backend
	provider spec.Provider
	maxInput int
}{
	{
		name:     "openai",
		backend:  NewOpenAI(spec.ProviderConfig{Adapter: "openai", MaxInputChars: 12000}, nil),
		provider: spec.ProviderOpenAI,
		maxInput: 12000,
	},
	{
		name:     "anthropic",
		backend:  NewAnthropic(spec.ProviderConfig{Adapter: "anthropic", MaxInputChars: 8000}, nil),
		provider: spec.ProviderAnthropic,
		maxInput: 8000,
	},
	{
		name:     "ollama",
		backend:  NewOllama(spec.ProviderConfig{Adapter: "ollama", MaxInputChars: 4000}, nil),
		provider: spec.ProviderOllama,
		maxInput: 4000,
	},
	{
		name:     "llamacpp",
		backend:  NewLlamaCpp(spec.ProviderConfig{Adapter: "llamacpp", MaxInputChars: 3000}, nil),
		provider: spec.ProviderLlamaCpp,
		maxInput: 3000,
	},
	{
		name:     "horde",
		backend:  NewHorde(spec.ProviderConfig{Adapter: "horde", MaxInputChars: 2000}, nil),
		provider: spec.ProviderHorde,
		maxInput: 2000,
	},
	{
		// 16000 is the configured gateway truncation budget (MaxInputChars → Router.applyTruncation), not a Gemini API character limit.
		name:     "gemini",
		backend:  NewOpenAI(spec.ProviderConfig{Adapter: "gemini", MaxInputChars: 16000}, nil),
		provider: spec.ProviderGemini,
		maxInput: 16000,
	},
	{
		name:     "custom openai name",
		backend:  NewOpenAI(spec.ProviderConfig{Name: "poolside", Adapter: "openai", MaxInputChars: 16000}, nil),
		provider: spec.Provider("poolside"),
		maxInput: 16000,
	},
}

func TestBackendIdentity(t *testing.T) {
	t.Parallel()

	for _, tt := range backendIdentityCases {
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
