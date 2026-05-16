package worker

import (
	"context"
	"strings"
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


// newRoutingTest creates a Worker with mock dependencies for routing tests.
func newRoutingTest(profile models.AgentProfile) (*Worker, *routingTestStore, *routingTestGateway, *routingTestSandbox) {
	store := &routingTestStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "task-routing"},
			ProjectID:  "project-1",
			AgentID:    "agent-1",
			State:      models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "project-1"},
			WorkspacePath: "/tmp/test-workspace",
		},
		profile: profile,
	}
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	return w, store, gw, sb
}

// TestRoutingDecision_AgenticModeFalse_LegacyPath verifies by calling Process
// that when AgenticMode is false, the gateway receives JSONMode requests (legacy path).
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeFalse_LegacyPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"OpenAI", "openai"},
		{"Anthropic", "anthropic"},
		{"Ollama", "ollama"},
		{"Empty provider", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    tc.provider,
				Model:       "gpt-4",
				AgenticMode: false,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			// Legacy path: gateway request should have JSONMode=true and no tools
			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if !gw.requests[0].JSONMode {
				t.Error("expected JSONMode=true for legacy path")
			}
			if len(gw.requests[0].Tools) > 0 {
				t.Error("expected no tools in legacy path request")
			}
		})
	}
}

func TestRoutingDecision_AgenticModeFalse_MakesOnlyLegacySingleCall(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: false,
	}
	w, store, gw, sb := newRoutingTest(profile)

	w.Process(context.Background(), store.task)

	if len(gw.requests) != 1 {
		t.Fatalf("expected exactly 1 legacy gateway call and zero agentic follow-up calls, got %d", len(gw.requests))
	}
	if !gw.requests[0].JSONMode {
		t.Fatal("expected legacy request to use JSON mode")
	}
	if len(gw.requests[0].Tools) != 0 {
		t.Fatalf("expected legacy request to advertise zero tools, got %d", len(gw.requests[0].Tools))
	}
	if sb.execCount != 1 {
		t.Fatalf("expected legacy command to execute once, got %d", sb.execCount)
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderSupported verifies by calling Process
// that when AgenticMode is true and provider supports agentic mode, the agentic path is taken.
// Validates: Requirements 1, 3, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderSupported(t *testing.T) {
	t.Parallel()

	for _, provider := range []string{"openai", "anthropic"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    provider,
				Model:       "gpt-4",
				AgenticMode: true,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if gw.requests[0].JSONMode {
				t.Error("expected JSONMode=false for agentic path")
			}
			if len(gw.requests[0].Tools) == 0 {
				t.Error("expected tools in agentic path request")
			}
		})
	}
}

// TestRoutingDecision_AgenticModeTrue_ProviderNotSupported verifies by calling Process
// that when AgenticMode is true but provider doesn't support it, the legacy path is taken.
// Validates: Requirements 1, 3, 4, 6.2
func TestRoutingDecision_AgenticModeTrue_ProviderNotSupported(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		provider string
	}{
		{"Ollama", "ollama"},
		{"Azure OpenAI", "azure-openai"},
		{"Vertex", "vertex"},
		{"Empty provider", ""},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			profile := models.AgentProfile{
				ID:          "agent-1",
				Provider:    tc.provider,
				Model:       "claude-3",
				AgenticMode: true,
			}
			w, store, gw, _ := newRoutingTest(profile)
			w.Process(context.Background(), store.task)

			// Fallback to legacy: should use JSONMode, no tools
			if len(gw.requests) == 0 {
				t.Fatal("expected at least 1 gateway request")
			}
			if !gw.requests[0].JSONMode {
				t.Errorf("expected JSONMode=true for legacy fallback with provider %q", tc.provider)
			}
			if len(gw.requests[0].Tools) > 0 {
				t.Errorf("expected no tools in legacy fallback for provider %q", tc.provider)
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

// TestLegacyPath_DoesNotUseTruncation is a smoke test that verifies the legacy
// (non-agentic) worker path does not apply any truncation. Legacy mode uses
// GenerateJSON with single-shot command execution and should not be affected
// by the agentic truncation feature.
// Validates: Requirement 6.2 (Non-Goals: Modifying legacy worker truncation behavior)
func TestLegacyPath_DoesNotUseTruncation(t *testing.T) {
	t.Parallel()

	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "ollama", // Non-agentic provider
		Model:       "claude-3",
		AgenticMode: false, // Explicitly disable agentic mode
	}
	w, store, gw, _ := newRoutingTest(profile)
	w.Process(context.Background(), store.task)

	// Verify legacy path was taken (should have exactly 1 request)
	if len(gw.requests) != 1 {
		t.Fatalf("expected 1 gateway request for legacy path, got %d", len(gw.requests))
	}

	req := gw.requests[0]

	// Legacy path characteristics:
	// 1. JSONMode should be true (single-shot command)
	if !req.JSONMode {
		t.Error("expected JSONMode=true for legacy path")
	}

	// 2. No tools should be advertised (legacy doesn't support tool calls)
	if len(req.Tools) != 0 {
		t.Error("expected no tools in legacy path request")
	}

	// 3. No truncation-related markers should appear in messages
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, "【") || strings.Contains(msg.Content, "collapsed") {
			t.Error("legacy path should not contain truncation markers")
		}
	}
}

// TestLegacyPath_NotAffectedByAgenticConfig verifies that even when the Worker
// is configured with agentic truncation settings, the legacy path does not
// apply truncation. This ensures the non-goal of "Modifying legacy worker
// truncation behavior" is met.
// Validates: Non-Goal: Modifying legacy worker truncation behavior
func TestLegacyPath_NotAffectedByAgenticConfig(t *testing.T) {
	t.Parallel()

	// Create worker with agentic truncation config
	profile := models.AgentProfile{
		ID:          "agent-1",
		Provider:    "openai", // Even with OpenAI provider
		Model:       "gpt-4",
		AgenticMode: false, // But legacy mode
	}
	store := &routingTestStore{
		task: models.Task{
			BaseEntity: models.BaseEntity{ID: "task-legacy-config"},
			ProjectID:  "project-1",
			AgentID:    "agent-1",
			State:      models.TaskStateQueued,
		},
		project: models.Project{
			BaseEntity:    models.BaseEntity{ID: "project-1"},
			WorkspacePath: "/tmp/test-workspace",
		},
		profile: profile,
	}
	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}

	// Configure with agentic truncation settings
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		MaxRetries:              3,
		MaxToolIterations:       50,
		AgenticTruncatorMax:     30,     // Agentic config
		AgenticTruncationThresh: 40,     // Agentic config
		AgenticCharacterBudget:  100000, // Agentic config
	})

	w.Process(context.Background(), store.task)

	// Should still take legacy path due to AgenticMode: false
	if len(gw.requests) != 1 {
		t.Fatalf("expected 1 gateway request, got %d", len(gw.requests))
	}

	// Verify legacy path characteristics even with agentic config
	req := gw.requests[0]
	if !req.JSONMode {
		t.Error("legacy path should use JSONMode even with agentic config")
	}
	if len(req.Tools) != 0 {
		t.Error("legacy path should not have tools even with agentic config")
	}
}
