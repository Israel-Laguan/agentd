package worker

import (
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// TestProviderSupportsAgentic_ReturnsTrueForOpenAI verifies that providerSupportsAgentic
// returns true for OpenAI provider.
// Validates: Requirements 1, 3, 4, 6.2
func TestProviderSupportsAgentic_ReturnsTrueForOpenAI(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
		expected bool
	}{
		{"OpenAI lowercase", "openai", true},
		{"OpenAI uppercase", "OPENAI", true},
		{"OpenAI mixed case", "OpenAI", true},
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
// returns false for providers other than OpenAI.
// Validates: Requirements 1, 3, 4, 6.2
func TestProviderSupportsAgentic_ReturnsFalseForOtherProviders(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Anthropic", "anthropic"},
		{"Anthropic uppercase", "ANTHROPIC"},
		{"Anthropic mixed case", "Anthropic"},
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

// TestRoutingDecision_AgenticModeFalse_LegacyPath verifies the routing decision
// when AgenticMode is false (default). The worker should NOT route to agentic path.
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeFalse_LegacyPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		agenticMode bool
		provider    string
		wantAgentic bool
	}{
		{"AgenticMode false, OpenAI", false, "openai", false},
		{"AgenticMode false, Anthropic", false, "anthropic", false},
		{"AgenticMode false, Ollama", false, "ollama", false},
		{"AgenticMode false, empty provider", false, "", false},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &Worker{}
			profile := models.AgentProfile{
				ID:         "test",
				Provider:   tc.provider,
				Model:      "gpt-4",
				AgenticMode: tc.agenticMode,
			}

			// When AgenticMode is false, providerSupportsAgentic result is irrelevant
			// because the routing logic checks AgenticMode first
			providerSupports := w.providerSupportsAgentic(profile)
			
			// Simulate the routing logic from Worker.Process
			shouldRouteToAgentic := profile.AgenticMode && providerSupports

			if shouldRouteToAgentic != tc.wantAgentic {
				t.Errorf("routing decision = %v, want %v", shouldRouteToAgentic, tc.wantAgentic)
			}
		})
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderSupported routes to agentic path
// when AgenticMode is true and provider supports (OpenAI).
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderSupported(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	profile := models.AgentProfile{
		ID:         "test",
		Provider:   "openai",
		Model:      "gpt-4",
		AgenticMode: true,
	}

	providerSupports := w.providerSupportsAgentic(profile)
	shouldRouteToAgentic := profile.AgenticMode && providerSupports

	if !shouldRouteToAgentic {
		t.Error("expected routing to agentic path when AgenticMode is true and provider supports")
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderNotSupported verifies fallback to legacy
// when AgenticMode is true but provider doesn't support agentic mode.
// Validates: Requirements 1, 3, 4, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderNotSupported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Anthropic", "anthropic"},
		{"Ollama", "ollama"},
		{"Azure OpenAI", "azure-openai"},
		{"Vertex", "vertex"},
		{"Empty provider", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := &Worker{}
			profile := models.AgentProfile{
				ID:         "test",
				Provider:   tc.provider,
				Model:      "claude-3",
				AgenticMode: true,
			}

			providerSupports := w.providerSupportsAgentic(profile)
			shouldRouteToAgentic := profile.AgenticMode && providerSupports

			if shouldRouteToAgentic {
				t.Errorf("expected fallback to legacy when AgenticMode is true but provider %q doesn't support", tc.provider)
			}
		})
	}
}

// TestAgenticMode_DefaultIsFalse verifies that the default value of AgenticMode is false.
// Validates: Requirement 2.1, 2.2
func TestAgenticMode_DefaultIsFalse(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{}
	if profile.AgenticMode != false {
		t.Errorf("AgenticMode default = %v, want false", profile.AgenticMode)
	}
}

// TestAgenticMode_CanBeSet verifies that AgenticMode can be set to true.
// Validates: Requirement 1.1
func TestAgenticMode_CanBeSet(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{AgenticMode: true}
	if !profile.AgenticMode {
		t.Error("AgenticMode should be settable to true")
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