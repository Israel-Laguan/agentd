package worker

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/gateway"
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
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, "【") || strings.Contains(msg.Content, "collapsed") {
			t.Error("legacy path should not contain truncation markers even with agentic config")
		}
	}
}

// TestLegacySystemPrompt_StatesOneCommandConstraint verifies that the default
// legacy system prompt explicitly states the one-command constraint and
// discourages embedding large output in command arguments.
func TestLegacySystemPrompt_StatesOneCommandConstraint(t *testing.T) {
	t.Parallel()

	content := legacyJSONCommandSystemContent(models.AgentProfile{})

	checks := []struct {
		phrase string
		reason string
	}{
		{"one JSON object", "prompt must state the single-object constraint"},
		{"CONSTRAINT", "prompt must have an explicit CONSTRAINT section"},
		{"pipes", "prompt must recommend pipes/redirects over embedded output"},
		{"non-interactive", "prompt must require non-interactive flags"},
	}
	for _, c := range checks {
		if !strings.Contains(content, c.phrase) {
			t.Errorf("system prompt missing %q: %s", c.phrase, c.reason)
		}
	}

	// Sentinel used by tests as a probe must still match the first sentence.
	if !strings.HasPrefix(content, legacyJSONCommandSystemSentinel) {
		t.Errorf("prompt does not start with sentinel %q", legacyJSONCommandSystemSentinel)
	}
}

// legacyHandoffStore extends routingTestStore to capture BlockTaskWithSubtasks calls.
type legacyHandoffStore struct {
	routingTestStore
	mu          sync.Mutex
	blocked  bool
	subtasks []models.DraftTask
}

func (s *legacyHandoffStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, drafts []models.DraftTask) (*models.Task, []models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocked = true
	s.subtasks = append(s.subtasks, drafts...)
	s.task.State = models.TaskStateBlocked
	return &s.task, nil, nil
}

// legacyHandoffSink records emitted event kinds.
type legacyHandoffSink struct {
	mu    sync.Mutex
	kinds []string
}

func (s *legacyHandoffSink) Emit(_ context.Context, ev models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kinds = append(s.kinds, string(ev.Type))
	return nil
}

// invalidJSONGateway returns non-JSON content to simulate truncated responses.
type invalidJSONGateway struct {
	routingTestGateway
}

func (g *invalidJSONGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.requests = append(g.requests, req)
	// Return something that cannot be parsed as JSON.
	return gateway.AIResponse{Content: `{"command":"echo '...truncated`}, nil
}

func newLegacyJSONHandoffFixtures() (*legacyHandoffStore, *invalidJSONGateway, *legacyHandoffSink) {
	profile := models.AgentProfile{
		ID: "agent-handoff", Provider: "ollama", Model: "llama3", AgenticMode: false,
	}
	store := &legacyHandoffStore{
		routingTestStore: routingTestStore{
			task: models.Task{
				BaseEntity: models.BaseEntity{ID: "task-json-fail"},
				ProjectID:  "project-1", AgentID: "agent-handoff", State: models.TaskStateQueued,
			},
			project: models.Project{
				BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace",
			},
			profile: profile,
		},
	}
	return store, &invalidJSONGateway{}, &legacyHandoffSink{}
}

// TestLegacyWorker_InvalidJSONResponse_EmitsHumanHandoff verifies that when
// the gateway persistently returns invalid JSON (simulating a truncated
// response), the legacy worker creates a HUMAN-assignee handoff subtask and
// emits a LEGACY_MODE_HANDOFF event instead of cycling through generic retries.
func TestLegacyWorker_InvalidJSONResponse_EmitsHumanHandoff(t *testing.T) {
	t.Parallel()

	store, gw, sink := newLegacyJSONHandoffFixtures()
	w := NewWorker(store, gw, &routingTestSandbox{}, nil, sink, WorkerOptions{MaxToolIterations: 5})
	w.Process(context.Background(), store.task)

	store.mu.Lock()
	blocked, subtasks := store.blocked, store.subtasks
	store.mu.Unlock()

	if !blocked {
		t.Fatal("expected BlockTaskWithSubtasks to be called for invalid JSON handoff")
	}
	if len(subtasks) == 0 {
		t.Fatal("expected at least one handoff subtask")
	}
	if subtasks[0].Assignee != models.TaskAssigneeHuman {
		t.Errorf("handoff subtask assignee = %q, want %q", subtasks[0].Assignee, models.TaskAssigneeHuman)
	}
	if !strings.Contains(subtasks[0].Description, "agentic_mode") {
		t.Errorf("handoff description missing 'agentic_mode'; got: %s", subtasks[0].Description[:min(200, len(subtasks[0].Description))])
	}
	sink.mu.Lock()
	kinds := sink.kinds
	sink.mu.Unlock()
	if !containsEvent(kinds, "LEGACY_MODE_HANDOFF") {
		t.Errorf("expected LEGACY_MODE_HANDOFF event; got events: %v", kinds)
	}
}

func containsEvent(kinds []string, want string) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
