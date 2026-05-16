package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/capabilities"
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
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	store.profile = profile
	store.project = models.Project{
		BaseEntity:    models.BaseEntity{ID: "project-1"},
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
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	store.profile = profile
	store.project = models.Project{
		BaseEntity:    models.BaseEntity{ID: "project-1"},
		WorkspacePath: "/tmp/test-workspace",
	}

	w.Process(context.Background(), task)

	if alwaysToolCallsGateway.callCount != 3 {
		t.Errorf("expected 3 gateway calls before iteration guard stopped the loop, got %d", alwaysToolCallsGateway.callCount)
	}

	if store.task.RetryCount != 1 {
		t.Errorf("expected task retry count incremented after max iterations, got %d", store.task.RetryCount)
	}
	if store.task.State != models.TaskStateReady {
		t.Errorf("expected task requeued after max iterations, got state %q", store.task.State)
	}
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
		ID:          "agent-1",
		Provider:    "openai",
		Model:       "gpt-4",
		AgenticMode: true,
	}
	store.profile = profile
	store.project = models.Project{
		BaseEntity:    models.BaseEntity{ID: "project-1"},
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

func TestAgenticLoop_InvokesCapabilityRegistryAndAccumulatesMessages(t *testing.T) {
	t.Parallel()

	mockGateway := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				Content: "I will call the no-op tool.",
				ToolCalls: []gateway.ToolCall{
					{ID: "call_noop", Type: "function", Function: gateway.ToolCallFunction{Name: "noop", Arguments: `{"value":"probe"}`}},
				},
			},
			{Content: "No-op complete."},
		},
	}
	mockSandbox := &mockAgenticSandbox{}
	store := newMockAgenticStore("task-capability-registry")
	adapter := &recordingCapabilityAdapter{
		tools: []gateway.ToolDefinition{
			{
				Name:        "noop",
				Description: "No-op test tool.",
				Parameters:  &gateway.FunctionParameters{Type: "object"},
			},
		},
		result: map[string]any{"ok": true},
	}
	registry := capabilities.NewRegistry()
	registry.Register("recording", adapter)

	w := NewWorker(store, mockGateway, mockSandbox, nil, nil, WorkerOptions{
		MaxToolIterations: 5,
		Capabilities:      registry,
	})

	w.Process(context.Background(), store.task)

	if mockGateway.callCount != 2 {
		t.Fatalf("expected 2 gateway calls, got %d", mockGateway.callCount)
	}
	if adapter.callCount != 1 {
		t.Fatalf("expected capability registry tool to be invoked once, got %d", adapter.callCount)
	}
	if adapter.lastName != "noop" || adapter.lastArgs["value"] != "probe" {
		t.Fatalf("unexpected capability call name=%q args=%#v", adapter.lastName, adapter.lastArgs)
	}
	if mockSandbox.executionCount != 0 {
		t.Fatalf("expected no sandbox calls for capability tool, got %d", mockSandbox.executionCount)
	}
	if store.committedResult == nil || !store.committedResult.Success || !strings.Contains(store.committedResult.Payload, "No-op complete.") {
		t.Fatalf("expected final text committed successfully, got %#v", store.committedResult)
	}

	if len(mockGateway.requests) != 2 {
		t.Fatalf("expected 2 recorded gateway requests, got %d", len(mockGateway.requests))
	}
	if !requestContainsTool(mockGateway.requests[0], "noop") {
		t.Fatalf("expected first request to advertise noop tool")
	}
	if !requestContainsToolResult(mockGateway.requests[1], "call_noop", `"ok":true`) {
		t.Fatalf("expected second request to include noop tool result, got %#v", mockGateway.requests[1].Messages)
	}
}

func TestAgenticLoop_ToolErrorStringStillContinuesToFinalResponse(t *testing.T) {
	t.Parallel()

	mockGateway := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				Content: "I will run a command.",
				ToolCalls: []gateway.ToolCall{
					{ID: "call_fail", Type: "function", Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"false"}`}},
				},
			},
			{Content: "The command failed, so I am reporting the failure."},
		},
	}
	mockSandbox := &mockAgenticSandbox{
		results: map[string]sandbox.Result{
			"false": {Success: false, ExitCode: 1, Stdout: "", Stderr: "boom"},
		},
	}
	store := newMockAgenticStore("task-tool-error")
	w := NewWorker(store, mockGateway, mockSandbox, nil, nil, WorkerOptions{MaxToolIterations: 5})

	w.Process(context.Background(), store.task)

	if mockGateway.callCount != 2 {
		t.Fatalf("expected model to continue after tool error with a second call, got %d calls", mockGateway.callCount)
	}
	if mockSandbox.executionCount != 1 {
		t.Fatalf("expected one sandbox call, got %d", mockSandbox.executionCount)
	}
	if !requestContainsToolResult(mockGateway.requests[1], "call_fail", "command failed with exit code 1") {
		t.Fatalf("expected second request to include tool error string, got %#v", mockGateway.requests[1].Messages)
	}
	if store.committedResult == nil || !store.committedResult.Success {
		t.Fatalf("expected final response committed after tool error, got %#v", store.committedResult)
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

