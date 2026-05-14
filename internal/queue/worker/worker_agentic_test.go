package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func TestBuildAgenticMessagesReplacesLeadingSystemMessage(t *testing.T) {
	w := &Worker{}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "existing prompt"},
		{Role: "user", Content: "do work"},
	}
	profile := models.AgentProfile{
		SystemPrompt: sql.NullString{String: "custom prompt", Valid: true},
	}

	got := w.buildAgenticMessages(messages, profile)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "system" {
		t.Fatalf("expected first role system, got %q", got[0].Role)
	}
	if got[1].Role != "user" {
		t.Fatalf("expected second role user, got %q", got[1].Role)
	}
	want := "custom prompt\n\n" + agenticToolUseSystemText()
	if got[0].Content != want {
		t.Fatalf("expected system message content %q, got %q", want, got[0].Content)
	}
	if countRole(got, "system") != 1 {
		t.Fatalf("expected one system message, got %d", countRole(got, "system"))
	}
}

func TestBuildAgenticMessagesInsertsBeforeFirstUserWhenNoLeadingSystem(t *testing.T) {
	w := &Worker{}
	messages := []gateway.PromptMessage{
		{Role: "assistant", Content: "prior response"},
		{Role: "user", Content: "run task"},
	}

	got := w.buildAgenticMessages(messages, models.AgentProfile{})
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[1].Role != "system" {
		t.Fatalf("expected inserted message at index 1 to be system, got %q", got[1].Role)
	}
	if got[2].Role != "user" {
		t.Fatalf("expected user message after inserted system, got %q", got[2].Role)
	}
}

func TestBuildAgenticMessagesPreservesMemoryLessonsAndReplacesLegacy(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	lessons := "LESSONS LEARNED (from previous tasks):\n1. Symptom: x\n   Solution: y\n"
	legacy := legacyJSONCommandSystemSentinel + `, {"command":"..."}, or if the task is too complex...`
	messages := []gateway.PromptMessage{
		{Role: "system", Content: lessons},
		{Role: "system", Content: legacy},
		{Role: "user", Content: "task body"},
	}
	got := w.buildAgenticMessages(messages, models.AgentProfile{})
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
	if got[0].Content != lessons {
		t.Fatalf("expected memory lessons preserved, got %q", got[0].Content)
	}
	if got[0].Role != "system" || got[1].Role != "system" || got[2].Role != "user" {
		t.Fatalf("unexpected roles: %+v", got)
	}
	if isLegacyJSONCommandSystemPrompt(got[1].Content) {
		t.Fatalf("expected legacy JSON prompt removed from agentic system, got %q", got[1].Content)
	}
	if !strings.Contains(got[1].Content, "You are an autonomous agent") || !strings.Contains(got[1].Content, "bash tool") {
		t.Fatalf("expected agentic tool instructions in second system, got %q", got[1].Content)
	}
	if got[1].Content != agenticToolUseSystemText() {
		t.Fatalf("expected bare agentic system when profile empty, got %q", got[1].Content)
	}
}

func TestExecuteAgenticTool_CapabilityRegistry(t *testing.T) {
	t.Parallel()
	registry := capabilities.NewRegistry()
	registry.Register("fake", fakeCapabilityCallAdapter{
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
	if len(tools) != 5 {
		t.Fatalf("expected 5 tools total (3 executor + 1 delegate + 1 capability), got %d", len(tools))
	}
	if !containsTool(tools, "bash") {
		t.Fatal("expected bash tool from executor")
	}
	if !containsTool(tools, "capability_tool") {
		t.Fatal("expected capability tool")
	}
}

type fakeCapabilityAdapter struct {
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityAdapter) Name() string { return "fake" }

func (f fakeCapabilityAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityAdapter) CallTool(context.Context, string, map[string]any) (any, error) {
	return nil, nil
}

func (f fakeCapabilityAdapter) Close() error { return nil }

type fakeCapabilityCallAdapter struct {
	tools []gateway.ToolDefinition
}

func (f fakeCapabilityCallAdapter) Name() string { return "fake" }

func (f fakeCapabilityCallAdapter) ListTools(context.Context) ([]gateway.ToolDefinition, error) {
	return f.tools, nil
}

func (f fakeCapabilityCallAdapter) CallTool(_ context.Context, name string, args map[string]any) (any, error) {
	return map[string]any{"tool": name, "args": args}, nil
}

func (f fakeCapabilityCallAdapter) Close() error { return nil }

func containsTool(tools []gateway.ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func countRole(messages []gateway.PromptMessage, role string) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == role {
			count++
		}
	}
	return count
}

// TestProcessAgentic_BuildsAgenticMessages verifies that buildAgenticMessages correctly
// adds the agentic system prompt to messages.
// Validates: Requirements 5, 6.2
func TestProcessAgentic_BuildsAgenticMessages(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	messages := []gateway.PromptMessage{
		{Role: "user", Content: "create a file hello.txt"},
	}

	// Test with default profile (no custom system prompt)
	result := w.buildAgenticMessages(messages, models.AgentProfile{})

	// Should have inserted a system message at the beginning
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}

	if result[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %q", result[0].Role)
	}

	// Verify the agentic system text is present
	if !strings.Contains(result[0].Content, "You are an autonomous agent") {
		t.Error("expected agentic system prompt to contain autonomy statement")
	}
	if !strings.Contains(result[0].Content, "bash tool") {
		t.Error("expected agentic system prompt to mention bash tool")
	}

	// Verify original user message is preserved
	if result[1].Role != "user" {
		t.Fatalf("expected second message to be user, got %q", result[1].Role)
	}
	if result[1].Content != "create a file hello.txt" {
		t.Fatalf("expected user message content to be preserved, got %q", result[1].Content)
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

	// Should have 4 tools (bash, read, write, delegate)
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools when no capabilities, got %d", len(tools))
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

	expectedTools := []string{"bash", "read", "write", "delegate"}
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

// mockExecSandbox implements sandbox.Executor for testing
type mockExecSandbox struct {
	result sandbox.Result
	err    error
}

func (m *mockExecSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	return m.result, m.err
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

// mockCommitStore implements models.KanbanStore to support commitText testing
type mockCommitStore struct {
	text *string
}

func (m *mockCommitStore) MarkTaskRunning(ctx context.Context, id string, t time.Time, pid int) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskHeartbeat(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) IncrementRetryCount(ctx context.Context, id string, t time.Time) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskState(ctx context.Context, id string, t time.Time, state models.TaskState) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) UpdateTaskResult(ctx context.Context, id string, t time.Time, result models.TaskResult) (*models.Task, error) {
	if m.text != nil && result.Payload != "" {
		*m.text = result.Payload
	}
	return nil, nil
}

func (m *mockCommitStore) AddComment(ctx context.Context, c models.Comment) error {
	return nil
}

func (m *mockCommitStore) ListComments(ctx context.Context, id string) ([]models.Comment, error) {
	return nil, nil
}

func (m *mockCommitStore) GetProject(ctx context.Context, id string) (*models.Project, error) {
	return nil, nil
}

func (m *mockCommitStore) GetAgentProfile(ctx context.Context, id string) (*models.AgentProfile, error) {
	return nil, nil
}

func (m *mockCommitStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) Close() error {
	return nil
}

func (m *mockCommitStore) AppendEvent(ctx context.Context, e models.Event) error {
	return nil
}

func (m *mockCommitStore) ListEventsByTask(ctx context.Context, id string) ([]models.Event, error) {
	return nil, nil
}

func (m *mockCommitStore) MarkEventsCurated(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) DeleteCuratedEvents(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) ListCompletedTasksOlderThan(ctx context.Context, d time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) RecordMemory(ctx context.Context, mem models.Memory) error {
	return nil
}

func (m *mockCommitStore) ListMemories(ctx context.Context, f models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockCommitStore) RecallMemories(ctx context.Context, q models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockCommitStore) TouchMemories(ctx context.Context, ids []string) error {
	return nil
}

func (m *mockCommitStore) SupersedeMemories(ctx context.Context, oldIDs []string, newID string) error {
	return nil
}

func (m *mockCommitStore) ListUnsupersededMemories(ctx context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockCommitStore) UpsertAgentProfile(ctx context.Context, p models.AgentProfile) error {
	return nil
}

func (m *mockCommitStore) ListAgentProfiles(ctx context.Context) ([]models.AgentProfile, error) {
	return nil, nil
}

func (m *mockCommitStore) DeleteAgentProfile(ctx context.Context, id string) error {
	return nil
}

func (m *mockCommitStore) AssignTaskAgent(ctx context.Context, taskID string, t time.Time, agentID string) (*models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ListSettings(ctx context.Context) ([]models.Setting, error) {
	return nil, nil
}

func (m *mockCommitStore) GetSetting(ctx context.Context, key string) (string, bool, error) {
	return "", false, nil
}

func (m *mockCommitStore) SetSetting(ctx context.Context, key, value string) error {
	return nil
}

func (m *mockCommitStore) MaterializePlan(ctx context.Context, dp models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}

func (m *mockCommitStore) EnsureSystemProject(ctx context.Context) (*models.Project, error) {
	return nil, nil
}

func (m *mockCommitStore) EnsureProjectTask(ctx context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	return nil, false, nil
}

func (m *mockCommitStore) ListProjects(ctx context.Context) ([]models.Project, error) {
	return nil, nil
}

func (m *mockCommitStore) ListTasksByProject(ctx context.Context, projectID string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ClaimNextReadyTasks(ctx context.Context, limit int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ReconcileGhostTasks(ctx context.Context, alivePIDs []int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) ReconcileStaleTasks(ctx context.Context, alivePIDs []int, staleThreshold time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) AppendTasksToProject(ctx context.Context, projectID, parentTaskID string, drafts []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

func (m *mockCommitStore) BlockTaskWithSubtasks(ctx context.Context, taskID string, t time.Time, subtasks []models.DraftTask) (*models.Task, []models.Task, error) {
	return nil, nil, nil
}

func (m *mockCommitStore) ListUnprocessedHumanComments(ctx context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (m *mockCommitStore) MarkCommentProcessed(ctx context.Context, taskID, commentEventID string) error {
	return nil
}
