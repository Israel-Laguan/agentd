package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
)

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
		AgenticTruncatorMax:    30,     // Agentic config
		AgenticCharacterBudget: 100000, // Agentic config
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
