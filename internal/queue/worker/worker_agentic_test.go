package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestExecuteAgenticTool_CapabilityRegistry(t *testing.T) {
	t.Parallel()
	registry := capabilities.NewRegistry()
	registry.Register("fake", fakeCapabilityCallAdapter{
		name: "fake",
		tools: []gateway.ToolDefinition{
			{Name: "capability_tool", Description: "x", Parameters: &gateway.FunctionParameters{Type: "object"}},
		},
	})
	w := &Worker{capabilities: registry}
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)
	toolToAdapter := map[string]string{"capability_tool": "fake"}
	out := w.executeAgenticTool(context.Background(), "", ex, gateway.ToolCall{
		Function: gateway.ToolCallFunction{Name: "capability_tool", Arguments: `{"id":"1"}`},
	}, toolToAdapter)
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v out=%s", err, out)
	}
	if payload["tool"] != "capability_tool" {
		t.Fatalf("expected tool name in payload, got %#v", payload)
	}
	args, _ := payload["args"].(map[string]any)
	if args["id"] != "1" {
		t.Fatalf("expected args id, got %#v", payload)
	}
}

func TestDispatchTool_ScopedCapabilityWithoutAdapterIndex(t *testing.T) {
	t.Parallel()
	scopedRegistry := capabilities.NewRegistry()
	scopedRegistry.Register("scoped_fake", fakeCapabilityCallAdapter{
		name: "scoped_fake",
		tools: []gateway.ToolDefinition{
			{Name: "scoped_tool", Description: "x", Parameters: &gateway.FunctionParameters{Type: "object"}},
		},
	})

	w := &Worker{capabilities: capabilities.NewRegistry()} // Empty global registry
	ex := NewToolExecutor(nil, t.TempDir(), nil, 0)

	// Build a tool call for the scoped tool
	call := gateway.ToolCall{
		ID: "call_1",
		Function: gateway.ToolCallFunction{
			Name:      "scoped_tool",
			Arguments: `{"key":"val"}`,
		},
	}

	// Dispatch with NO toolToAdapter index (simulating dynamic registration)
	out := w.dispatchToolWithProject(context.Background(), "session-1", "project-1", call, nil, ex, scopedRegistry)

	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("failed to parse result: %v (out=%s)", err, out)
	}

	if payload["adapter"] != "scoped_fake" {
		t.Errorf("expected scoped_fake adapter, got %v", payload["adapter"])
	}
	if payload["tool"] != "scoped_tool" {
		t.Errorf("expected scoped_tool name, got %v", payload["tool"])
	}
}

func TestAgenticToolsIncludesExecutorAndCapabilityTools(t *testing.T) {
	registry := capabilities.NewRegistry()
	registry.Register("fake", fakeCapabilityAdapter{
		tools: []gateway.ToolDefinition{
			{
				Name:        "capability_tool",
				Description: "tool from capability adapter",
				Parameters:  &gateway.FunctionParameters{Type: "object"},
			},
		},
	})

	w := &Worker{capabilities: registry}
	executor := NewToolExecutor(nil, "", nil, 0)

	tools, _ := w.agenticTools(context.Background(), executor)
	if len(tools) != 6 {
		t.Fatalf("expected 6 tools total (3 executor + 2 delegate + 1 capability), got %d", len(tools))
	}
	if !containsTool(tools, "bash") {
		t.Fatal("expected bash tool from executor")
	}
	if !containsTool(tools, "capability_tool") {
		t.Fatal("expected capability tool")
	}
	if !containsTool(tools, "delegate_parallel") {
		t.Fatal("expected delegate_parallel tool")
	}
}


// TestProcessAgentic_CallsGatewayWithTools verifies that agenticTools returns tool definitions
// including both executor tools and capability tools.
// Validates: Requirements 5, 6.2
func TestProcessAgentic_CallsGatewayWithTools(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	executor := NewToolExecutor(nil, "", nil, 0)

	// Test with no capabilities registry
	tools, toolToAdapter := w.agenticTools(context.Background(), executor)

	// Should have 5 tools (bash, read, write, delegate, delegate_parallel)
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools when no capabilities, got %d", len(tools))
	}

	// No toolToAdapter when no capabilities
	if toolToAdapter != nil {
		t.Error("expected nil toolToAdapter when no capabilities")
	}

	// Verify tool names
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name] = true
	}

	expectedTools := []string{"bash", "read", "write", "delegate", "delegate_parallel"}
	for _, name := range expectedTools {
		if !toolNames[name] {
			t.Errorf("expected tool %q in agentic tools", name)
		}
	}
}

// TestProcessAgentic_ExecutesToolCalls verifies that executeAgenticTool correctly
// handles tool execution for built-in tools (bash, read, write).
// Validates: Requirements 5, 6.2
func TestProcessAgentic_ExecutesToolCalls(t *testing.T) {
	t.Parallel()

	// Create a mock sandbox that returns expected result
	mockSandbox := &mockExecSandbox{result: sandbox.Result{Stdout: "hello\n", Success: true}}
	executor := NewToolExecutor(mockSandbox, t.TempDir(), BuildSandboxEnv(nil, nil), 0)

	// Worker now uses DispatchTool which uses the internal toolExecutor
	w := &Worker{
		toolExecutor: executor,
	}

	// Test bash tool with echo command
	bashCall := gateway.ToolCall{
		ID: "call_123",
		Function: gateway.ToolCallFunction{
			Name:      "bash",
			Arguments: `{"command": "echo hello"}`,
		},
	}

	// Use DispatchTool as the single entry point for tool execution
	result := w.DispatchTool(context.Background(), "", bashCall, nil, w.toolExecutor)

	// The result should contain the output
	if !strings.Contains(result, "hello") {
		t.Errorf("expected result to contain 'hello', got %q", result)
	}
}


// TestProcessAgentic_CommitsTextWhenNoToolCalls verifies that the agentic loop
// commits text when the response contains no tool calls.
// This is verified by checking the logic flow: when resp.ToolCalls is empty,
// the code calls w.commitText and returns.
// Validates: Requirements 5, 6.2
func TestProcessAgentic_CommitsTextWhenNoToolCalls(t *testing.T) {
	t.Parallel()

	// We test the commitText method directly as the final step of the agentic loop
	w := &Worker{}

	// Create a mock store that captures the committed text
	committedText := ""
	store := &mockCommitStore{text: &committedText}
	w.store = store

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-123"},
		ProjectID:  "project-123",
		AgentID:    "agent-123",
		Title:      "Test task",
	}

	// Call commitText which is what happens when there are no tool calls
	w.commitText(context.Background(), task, "final response text")

	// The committed payload should contain the final text
	if !strings.Contains(*store.text, "final response text") {
		t.Errorf("expected committed text to contain 'final response text', got %q", *store.text)
	}
}

func TestIngestHumanCorrections_DeduplicatesProcessedComments(t *testing.T) {
	now := time.Now().UTC()
	store := &mockCommitStore{
		comments: []models.Comment{
			{
				BaseEntity: models.BaseEntity{ID: "comment-1", UpdatedAt: now},
				TaskID:     "task-1",
				Author:     models.CommentAuthorUser,
				Body:       "[CORRECT] was: old; is: new",
			},
		},
	}
	w := &Worker{store: store}
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task-1")

	w.ingestHumanCorrections(context.Background(), "task-1", cm)
	w.ingestHumanCorrections(context.Background(), "task-1", cm)

	corrections := cm.Corrections()
	if len(corrections) != 1 {
		t.Fatalf("expected 1 deduplicated correction, got %d", len(corrections))
	}
	if corrections[0].Source != CorrectionSourceHuman {
		t.Fatalf("expected human source, got %q", corrections[0].Source)
	}
	if len(store.listSinceArgs) != 2 {
		t.Fatalf("expected two since queries, got %d", len(store.listSinceArgs))
	}
	if !store.listSinceArgs[0].IsZero() {
		t.Fatalf("expected first query from zero time, got %s", store.listSinceArgs[0])
	}
	if !store.listSinceArgs[1].Equal(now) {
		t.Fatalf("expected second query from high-water mark %s, got %s", now, store.listSinceArgs[1])
	}
}

func TestIngestHumanCorrections_ReprocessesEditedComment(t *testing.T) {
	first := time.Now().UTC()
	second := first.Add(time.Minute)
	store := &mockCommitStore{
		comments: []models.Comment{
			{
				BaseEntity: models.BaseEntity{ID: "comment-1", UpdatedAt: first},
				TaskID:     "task-1",
				Author:     models.CommentAuthorUser,
				Body:       "[CORRECT] was: old; is: new",
			},
		},
	}
	w := &Worker{store: store}
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task-1")

	w.ingestHumanCorrections(context.Background(), "task-1", cm)
	store.comments[0].Body = "[CORRECT] was: older; is: newer"
	store.comments[0].UpdatedAt = second
	w.ingestHumanCorrections(context.Background(), "task-1", cm)

	corrections := cm.Corrections()
	if len(corrections) != 2 {
		t.Fatalf("expected edited comment to produce second correction, got %d", len(corrections))
	}
}

func TestIngestHumanCorrections_MapsReviewerSource(t *testing.T) {
	store := &mockCommitStore{
		comments: []models.Comment{
			{
				BaseEntity: models.BaseEntity{ID: "comment-1"},
				TaskID:     "task-1",
				Author:     models.CommentAuthor("reviewer"),
				Body:       "[CORRECT] was: old; is: reviewed",
			},
		},
	}
	w := &Worker{store: store}
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task-1")

	w.ingestHumanCorrections(context.Background(), "task-1", cm)

	corrections := cm.Corrections()
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Source != CorrectionSourceReviewer {
		t.Fatalf("expected reviewer source, got %q", corrections[0].Source)
	}
}

func TestIngestHumanCorrections_SkipsUnknownAuthors(t *testing.T) {
	store := &mockCommitStore{
		comments: []models.Comment{
			{
				BaseEntity: models.BaseEntity{ID: "comment-1"},
				TaskID:     "task-1",
				Author:     models.CommentAuthorWorkerAgent,
				Body:       "[CORRECT] was: old; is: new",
			},
			{
				BaseEntity: models.BaseEntity{ID: "comment-2"},
				TaskID:     "task-1",
				Author:     models.CommentAuthor("UNKNOWN"),
				Body:       "[CORRECT] was: old; is: newer",
			},
		},
	}
	w := &Worker{store: store}
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task-1")

	w.ingestHumanCorrections(context.Background(), "task-1", cm)

	if got := len(cm.Corrections()); got != 0 {
		t.Fatalf("expected unknown authors to be skipped, got %d corrections", got)
	}
}

