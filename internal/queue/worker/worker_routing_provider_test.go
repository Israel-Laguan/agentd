package worker

import (
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// TestProviderSupportsAgentic_ReturnsTrueForSupportedProviders verifies that providerSupportsAgentic
// returns true for OpenAI and Anthropic providers.
// Validates: Requirements 1, 3, 4, 6.2
func TestProviderSupportsAgentic_ReturnsTrueForSupportedProviders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
		expected bool
	}{
		{"OpenAI lowercase", "openai", true},
		{"OpenAI uppercase", "OPENAI", true},
		{"OpenAI mixed case", "OpenAI", true},
		{"Anthropic lowercase", "anthropic", true},
		{"Anthropic uppercase", "ANTHROPIC", true},
		{"Anthropic mixed case", "Anthropic", true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &Worker{}
			profile := models.AgentProfile{
				ID:       "test",
				Provider: tc.provider,
				Model:    "gpt-4",
			}

			result := w.providerSupportsAgentic(profile)
			if result != tc.expected {
				t.Errorf("providerSupportsAgentic(%q) = %v, want %v", tc.provider, result, tc.expected)
			}
		})
	}
}

// TestProviderSupportsAgentic_ReturnsFalseForOtherProviders verifies that providerSupportsAgentic
// returns false for providers other than OpenAI and Anthropic.
// Validates: Requirements 1, 3, 4, 6.2
func TestProviderSupportsAgentic_ReturnsFalseForOtherProviders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Ollama", "ollama"},
		{"Ollama uppercase", "OLLAMA"},
		{"Azure OpenAI", "azure-openai"},
		{"Vertex", "vertex"},
		{"Empty string", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &Worker{}
			profile := models.AgentProfile{
				ID:       "test",
				Provider: tc.provider,
				Model:    "claude-3",
			}

			result := w.providerSupportsAgentic(profile)
			if result {
				t.Errorf("providerSupportsAgentic(%q) = true, want false", tc.provider)
			}
		})
	}
}

// TestProviderSupportsAgentic_ImportFromGateway verifies that the provider constant
// is correctly imported from gateway package.
// Validates: Requirement 3.3
func TestProviderSupportsAgentic_ImportFromGateway(t *testing.T) {
	t.Parallel()

	// Verify gateway.ProviderOpenAI is accessible and has correct value
	if string(gateway.ProviderOpenAI) != "openai" {
		t.Errorf("gateway.ProviderOpenAI = %q, want \"openai\"", gateway.ProviderOpenAI)
	}
}
