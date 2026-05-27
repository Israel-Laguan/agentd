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
		// One entry per adapter type (wire protocol).  Do NOT add vendor alias names
		// here — aliases (e.g. "gemini") are covered by TestAppendFromConfig_LegacyGeminiAlias.
		{"openai", 1, false},
		{"anthropic", 1, false},
		{"ollama", 1, false},
		{"llamacpp", 1, false},
		{"horde", 1, false},
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

// TestAppendFromConfig_LegacyGeminiAlias is a regression guard for the "gemini"
// adapter alias.  "gemini" is not an adapter type — it is a vendor name that
// canonicalAdapter maps to the openai wire protocol.  This test must NOT be used
// as a template when adding new vendors; new OpenAI-compatible vendors should use
// adapter: openai with a custom name (see TestAppendFromConfig_CustomName).
func TestAppendFromConfig_LegacyGeminiAlias(t *testing.T) {
	t.Parallel()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{Adapter: "gemini", BaseURL: "http://example"})
	if err != nil {
		t.Fatalf("AppendFromConfig(gemini) error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name() != spec.ProviderGemini {
		t.Fatalf("Name() = %q, want %q", got[0].Name(), spec.ProviderGemini)
	}
	openAI, ok := got[0].(*OpenAI)
	if !ok {
		t.Fatalf("backend type = %T, want *OpenAI", got[0])
	}
	if openAI.cfg.Adapter != string(spec.ProviderOpenAI) {
		t.Fatalf("cfg.Adapter = %q, want openai", openAI.cfg.Adapter)
	}
}

func TestAppendFromConfig_CustomName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		adapter string
	}{
		{name: "synthetic-openai", adapter: "openai"},
		{name: "synthetic-anthropic", adapter: "anthropic"},
		{name: "synthetic-ollama", adapter: "ollama"},
		{name: "synthetic-llamacpp", adapter: "llamacpp"},
		{name: "synthetic-horde", adapter: "horde"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.adapter, func(t *testing.T) {
			t.Parallel()

			got, err := AppendFromConfig(nil, spec.ProviderConfig{
				Adapter: tt.adapter,
				Name:    tt.name,
				BaseURL: "http://example",
			})
			if err != nil {
				t.Fatalf("AppendFromConfig(%q) error = %v", tt.adapter, err)
			}
			if len(got) != 1 {
				t.Fatalf("len = %d, want 1", len(got))
			}
			if got[0].Name() != spec.Provider(tt.name) {
				t.Fatalf("Name() = %q, want %q", got[0].Name(), tt.name)
			}
		})
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

func TestAppendFromConfig_NormalizesAdapter(t *testing.T) {
	t.Parallel()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{Adapter: " OpenAI ", BaseURL: "http://example"})
	if err != nil {
		t.Fatalf("AppendFromConfig() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name() != spec.ProviderOpenAI {
		t.Fatalf("Name() = %q, want %q", got[0].Name(), spec.ProviderOpenAI)
	}
}

func TestAppendFromConfig_CustomNameNormalizesAdapter(t *testing.T) {
	t.Parallel()

	got, err := AppendFromConfig(nil, spec.ProviderConfig{
		Adapter: " OPENAI ",
		Name:    "poolside",
		BaseURL: "http://example",
	})
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
		// Regression guard: gemini is a vendor alias for the openai adapter.
		// 16000 is the configured gateway truncation budget, not a Gemini API limit.
		// Do NOT copy this pattern for new vendors — use adapter:openai + custom name instead.
		name:     "gemini (openai alias)",
		backend:  NewOpenAI(spec.ProviderConfig{Name: "gemini", Adapter: "openai", MaxInputChars: 16000}, nil),
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
