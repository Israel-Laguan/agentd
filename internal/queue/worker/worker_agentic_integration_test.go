package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// TestAgenticLoop_IntegrationWithMockGateway verifies the full agentic loop
// when the gateway returns a sequence: first response with tool_calls,
// second response with plain text (final result).
// Validates: Task 07 acceptance criteria - Integration-style test with mock gateway
// returning a sequence: first response tool_calls, second response plain text.
func TestAgenticLoop_IntegrationWithMockGateway(t *testing.T) {
	t.Parallel()

	// Create a mock gateway that returns a sequence of responses
	mockGateway := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				// First call: response with tool_calls (bash command)
				Content: "I'll execute a command to check the current directory.",
				ToolCalls: []gateway.ToolCall{
					{
						ID:   "call_abc123",
						Type: "function",
						Function: gateway.ToolCallFunction{
							Name:      "bash",
							Arguments: `{"command": "pwd"}`,
						},
					},
				},
				TokenUsage:   100,
				ProviderUsed: "openai",
				ModelUsed:    "gpt-4",
			},
			{
				// Second call: response with plain text (final result)
				Content:      "I have completed the task. The current working directory is /home/user.",
				ToolCalls:    nil,
				TokenUsage:   50,
				ProviderUsed: "openai",
				ModelUsed:    "gpt-4",
			},
		},
	}

	// Create a mock sandbox that executes bash commands
	mockSandbox := &mockAgenticSandbox{
		results: map[string]sandbox.Result{
			"pwd": {Success: true, ExitCode: 0, Stdout: "/home/user\n", Stderr: ""},
		},
	}

	// Create mock store
	store := &mockAgenticStore{}

	// Create worker with mocked dependencies
	w := NewWorker(
		store,
		mockGateway,
		mockSandbox,
		nil,
		nil,
		WorkerOptions{
			MaxToolIterations: 10,
		},
	)

	// Create a test task
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-integration-test"},
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		Title:      "Check current directory",
		State:      models.TaskStateQueued,
	}

	profile := models.AgentProfile{
		ID:         "agent-1",
		Provider:   "openai",
		Model:      "gpt-4",
		AgenticMode: true,
	}
	store.profile = profile
	store.project = models.Project{
		BaseEntity:     models.BaseEntity{ID: "project-1"},
		WorkspacePath: "/tmp/test-workspace",
	}

	// Process the task (this will call processAgentic internally)
	w.Process(context.Background(), task)

	// Verify the gateway was called twice (tool call + final response)
	if mockGateway.callCount != 2 {
		t.Errorf("expected 2 gateway calls, got %d", mockGateway.callCount)
	}

	// Verify first request had tools
	if len(mockGateway.requests) < 1 {
		t.Fatal("expected at least 1 gateway request")
	}
	if len(mockGateway.requests[0].Tools) == 0 {
		t.Error("first gateway request should include tool definitions")
	}

	// Verify iteration counter works - first iteration had tool calls
	if mockGateway.callCount < 1 {
		t.Error("gateway should have been called at least once")
	}

	// Verify the final result was committed
	if store.committedResult == nil {
		t.Error("expected a result to be committed")
	} else if !store.committedResult.Success {
		t.Error("expected successful result")
	}
}

// TestAgenticLoop_MaxIterationsRespected verifies that the worker respects
// the max tool iterations limit when the gateway always returns tool calls.
// Validates: Task 07 - Worker respects max iterations
func TestAgenticLoop_MaxIterationsRespected(t *testing.T) {
	t.Parallel()

	// Create a mock gateway that returns exactly 4 tool calls (more than max iterations)
	// This will cause the iteration guard to trigger after 3 iterations
	alwaysToolCallsGateway := &maxIterationsGateway{
		callCount: 0,
	}

	// Create mock sandbox
	mockSandbox := &mockAgenticSandbox{
		results: map[string]sandbox.Result{
			"echo 1": {Success: true, ExitCode: 0, Stdout: "1\n", Stderr: ""},
			"echo 2": {Success: true, ExitCode: 0, Stdout: "2\n", Stderr: ""},
			"echo 3": {Success: true, ExitCode: 0, Stdout: "3\n", Stderr: ""},
		},
	}

	store := &mockAgenticStore{}

	// Create worker with max iterations = 3
	w := NewWorker(
		store,
		alwaysToolCallsGateway,
		mockSandbox,
		nil,
		nil,
		WorkerOptions{
			MaxToolIterations: 3,
		},
	)

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-max-iter"},
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		Title:      "Test max iterations",
		State:      models.TaskStateQueued,
	}

	profile := models.AgentProfile{
		ID:         "agent-1",
		Provider:   "openai",
		Model:      "gpt-4",
		AgenticMode: true,
	}
	store.profile = profile
	store.project = models.Project{
		BaseEntity:     models.BaseEntity{ID: "project-1"},
		WorkspacePath: "/tmp/test-workspace",
	}

	w.Process(context.Background(), task)

	// Gateway should be called 4 times: 3 normal iterations + 1 final with injected message
	// The iteration guard allows one more call after the limit is hit
	if alwaysToolCallsGateway.callCount != 4 {
		t.Errorf("expected 4 gateway calls (3 normal + 1 final), got %d", alwaysToolCallsGateway.callCount)
	}

	// Note: handleAgentFailure triggers a retry rather than immediate failure.
	// The task state is updated to requeue, so committedResult may be nil.
	// The key verification is that gateway was called only 4 times (not unbounded).
}

// maxIterationsGateway always returns tool calls, simulating a gateway that
// keeps requesting tool execution (used for testing max iterations)
type maxIterationsGateway struct {
	callCount int
}

func (m *maxIterationsGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	m.callCount++
	return gateway.AIResponse{
		Content: fmt.Sprintf("Executing tool %d", m.callCount),
		ToolCalls: []gateway.ToolCall{
			{ID: fmt.Sprintf("call_%d", m.callCount), Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: fmt.Sprintf(`{"command": "echo %d"}`, m.callCount)}},
		},
	}, nil
}

func (m *maxIterationsGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}

func (m *maxIterationsGateway) AnalyzeScope(ctx context.Context, userIntent string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (m *maxIterationsGateway) ClassifyIntent(ctx context.Context, userIntent string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

// TestAgenticLoop_AppendsToolResultMessages verifies that tool results are
// appended to the conversation after tool execution.
// Validates: Task 07 - First iteration executes tool, appends tool result message
func TestAgenticLoop_AppendsToolResultMessages(t *testing.T) {
	t.Parallel()

	// Gateway sequence: tool call -> final response
	mockGateway := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				Content: "Let me run a command.",
				ToolCalls: []gateway.ToolCall{
					{ID: "call_test", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "ls"}`}},
				},
			},
			{
				Content:      "I see the files in the directory.",
				ToolCalls:    nil,
				TokenUsage:   50,
				ProviderUsed: "openai",
			},
		},
	}

	mockSandbox := &mockAgenticSandbox{
		results: map[string]sandbox.Result{
			"ls": {Success: true, ExitCode: 0, Stdout: "file1.txt\nfile2.txt\n", Stderr: ""},
		},
	}

	store := &mockAgenticStore{}

	w := NewWorker(
		store,
		mockGateway,
		mockSandbox,
		nil,
		nil,
		WorkerOptions{MaxToolIterations: 5},
	)

	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-tool-results"},
		ProjectID:  "project-1",
		AgentID:    "agent-1",
		Title:      "List files",
		State:      models.TaskStateQueued,
	}

	profile := models.AgentProfile{
		ID:         "agent-1",
		Provider:   "openai",
		Model:      "gpt-4",
		AgenticMode: true,
	}
	store.profile = profile
	store.project = models.Project{
		BaseEntity:     models.BaseEntity{ID: "project-1"},
		WorkspacePath: "/tmp/test-workspace",
	}

	w.Process(context.Background(), task)

	// Verify two gateway calls (tool + final)
	if mockGateway.callCount != 2 {
		t.Errorf("expected 2 gateway calls, got %d", mockGateway.callCount)
	}

	// Verify sandbox was called once (for the tool execution)
	if mockSandbox.executionCount != 1 {
		t.Errorf("expected 1 sandbox execution, got %d", mockSandbox.executionCount)
	}

	// Verify result was committed
	if store.committedResult == nil {
		t.Error("expected a result to be committed")
	}

	// Verify second gateway request includes tool-result message(s)
	if len(mockGateway.requests) < 2 {
		t.Fatalf("expected at least 2 gateway requests, got %d", len(mockGateway.requests))
	}
	foundToolMessage := false
	for _, msg := range mockGateway.requests[1].Messages {
		if msg.Role == "tool" {
			foundToolMessage = true
			if !strings.Contains(msg.Content, "file1.txt") {
				t.Errorf("expected tool message content to contain sandbox output 'file1.txt', got %q", msg.Content)
			}
			break
		}
	}
	if !foundToolMessage {
		t.Error("expected second gateway request to include a tool-result message with role 'tool'")
	}
}

// sequenceGateway is a mock gateway that returns a predefined sequence of responses.
// Used for testing the agentic loop that requires multiple gateway calls.
type sequenceGateway struct {
	responses []gateway.AIResponse
	callCount int
	requests  []gateway.AIRequest
}

func (m *sequenceGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	m.requests = append(m.requests, req)

	if m.callCount >= len(m.responses) {
		// Return a final response without tool calls to break the loop
		return gateway.AIResponse{Content: "No more responses"}, nil
	}

	resp := m.responses[m.callCount]
	m.callCount++
	return resp, nil
}

func (m *sequenceGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}

func (m *sequenceGateway) AnalyzeScope(ctx context.Context, userIntent string) (*gateway.ScopeAnalysis, error) {
	return nil, nil
}

func (m *sequenceGateway) ClassifyIntent(ctx context.Context, userIntent string) (*gateway.IntentAnalysis, error) {
	return nil, nil
}

// mockAgenticSandbox executes commands and returns predefined results
type mockAgenticSandbox struct {
	results         map[string]sandbox.Result
	executionCount  int
	lastCommand     string
}

func (m *mockAgenticSandbox) Execute(ctx context.Context, payload sandbox.Payload) (sandbox.Result, error) {
	m.executionCount++
	m.lastCommand = payload.Command

	// Look up the command in results
	if result, ok := m.results[payload.Command]; ok {
		return result, nil
	}

	// Default result if command not found
	return sandbox.Result{
		Success:  true,
		ExitCode: 0,
		Stdout:   "mock output",
		Stderr:   "",
	}, nil
}

// mockAgenticStore implements the store interface for agentic integration tests
type mockAgenticStore struct {
	task            models.Task
	project         models.Project
	profile         models.AgentProfile
	committedResult *models.TaskResult
}

func (m *mockAgenticStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, pid int) (*models.Task, error) {
	m.task.ID = id
	m.task.State = models.TaskStateRunning
	return &m.task, nil
}

func (m *mockAgenticStore) UpdateTaskHeartbeat(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) IncrementRetryCount(_ context.Context, _ string, _ time.Time) (*models.Task, error) {
	m.task.RetryCount++
	return &m.task, nil
}

func (m *mockAgenticStore) UpdateTaskState(_ context.Context, _ string, _ time.Time, next models.TaskState) (*models.Task, error) {
	m.task.State = next
	return &m.task, nil
}

func (m *mockAgenticStore) UpdateTaskResult(_ context.Context, _ string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	m.committedResult = &result
	if result.Success {
		m.task.State = models.TaskStateCompleted
	} else {
		m.task.State = models.TaskStateFailed
	}
	return &m.task, nil
}

func (m *mockAgenticStore) AddComment(context.Context, models.Comment) error {
	return nil
}

func (m *mockAgenticStore) ListComments(context.Context, string) ([]models.Comment, error) {
	return nil, nil
}

func (m *mockAgenticStore) GetProject(context.Context, string) (*models.Project, error) {
	return &m.project, nil
}

func (m *mockAgenticStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &m.profile, nil
}

func (m *mockAgenticStore) GetTask(context.Context, string) (*models.Task, error) {
	return &m.task, nil
}

func (m *mockAgenticStore) Close() error {
	return nil
}

func (m *mockAgenticStore) AppendEvent(context.Context, models.Event) error {
	return nil
}

func (m *mockAgenticStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}

func (m *mockAgenticStore) MarkEventsCurated(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) DeleteCuratedEvents(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) ListCompletedTasksOlderThan(_ context.Context, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) RecordMemory(context.Context, models.Memory) error {
	return nil
}

func (m *mockAgenticStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockAgenticStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockAgenticStore) TouchMemories(context.Context, []string) error {
	return nil
}

func (m *mockAgenticStore) SupersedeMemories(context.Context, []string, string) error {
	return nil
}

func (m *mockAgenticStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (m *mockAgenticStore) UpsertAgentProfile(context.Context, models.AgentProfile) error {
	return nil
}

func (m *mockAgenticStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{m.profile}, nil
}

func (m *mockAgenticStore) DeleteAgentProfile(context.Context, string) error {
	return nil
}

func (m *mockAgenticStore) AssignTaskAgent(_ context.Context, _ string, _ time.Time, _ string) (*models.Task, error) {
	return &m.task, nil
}

func (m *mockAgenticStore) ListSettings(context.Context) ([]models.Setting, error) {
	return nil, nil
}

func (m *mockAgenticStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (m *mockAgenticStore) SetSetting(context.Context, string, string) error {
	return nil
}

func (m *mockAgenticStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return nil, nil, nil
}

func (m *mockAgenticStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{}, nil
}

func (m *mockAgenticStore) EnsureProjectTask(context.Context, string, models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{}, true, nil
}

func (m *mockAgenticStore) ListProjects(context.Context) ([]models.Project, error) {
	return nil, nil
}

func (m *mockAgenticStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ClaimNextReadyTasks(context.Context, int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ReconcileGhostTasks(context.Context, []int) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) ReconcileStaleTasks(_ context.Context, _ []int, _ time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

func (m *mockAgenticStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, _ []models.DraftTask) (*models.Task, []models.Task, error) {
	return &m.task, nil, nil
}

func (m *mockAgenticStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (m *mockAgenticStore) MarkCommentProcessed(context.Context, string, string) error {
	return nil
}