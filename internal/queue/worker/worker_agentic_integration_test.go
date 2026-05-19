package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
func integrationSequenceResponses() []gateway.AIResponse {
	return []gateway.AIResponse{
		{
			Content: "I'll execute a command to check the current directory.",
			ToolCalls: []gateway.ToolCall{{
				ID: "call_abc123", Type: "function",
				Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "pwd"}`},
			}},
			TokenUsage: 100, ProviderUsed: "openai", ModelUsed: "gpt-4",
		},
		{
			Content:      "I have completed the task. The current working directory is /home/user.",
			TokenUsage:   50,
			ProviderUsed: "openai",
			ModelUsed:    "gpt-4",
		},
	}
}

func newAgenticIntegrationWorker(
	t *testing.T, gw *sequenceGateway, sb *mockAgenticSandbox, maxIter int,
) (*mockAgenticStore, *Worker, models.Task) {
	t.Helper()
	store := &mockAgenticStore{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: maxIter})
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-integration-test"},
		ProjectID:  "project-1", AgentID: "agent-1",
		Title: "Check current directory", State: models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace"}
	return store, w, task
}

func TestAgenticLoop_IntegrationWithMockGateway(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: integrationSequenceResponses()}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"pwd": {Success: true, ExitCode: 0, Stdout: "/home/user\n"},
	}}
	store, w, task := newAgenticIntegrationWorker(t, gw, sb, 10)
	w.Process(context.Background(), task)
	assertAgenticIntegrationOutcome(t, gw, store)
}

// TestAgenticLoop_EmitsToolAuditEvents verifies that Process() through the
// agentic inner loop emits TOOL_CALL and TOOL_RESULT via AuditHook.
func TestAgenticLoop_EmitsToolAuditEvents(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: integrationSequenceResponses()}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"pwd": {Success: true, ExitCode: 0, Stdout: "/home/user\n"},
	}}
	sink := &mockEventSink{}
	store := &mockAgenticStore{}
	w := NewWorker(store, gw, sb, nil, sink, WorkerOptions{MaxToolIterations: 10})
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-audit-events"},
		ProjectID:  "project-1", AgentID: "agent-1",
		Title: "Check current directory", State: models.TaskStateQueued,
	}
	store.task = task
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace"}

	w.Process(context.Background(), task)

	if len(sink.events) != 2 {
		t.Fatalf("expected 2 audit events, got %d", len(sink.events))
	}
	if sink.events[0].Type != models.EventTypeToolCall {
		t.Fatalf("first event should be TOOL_CALL, got %q", sink.events[0].Type)
	}
	if sink.events[1].Type != models.EventTypeToolResult {
		t.Fatalf("second event should be TOOL_RESULT, got %q", sink.events[1].Type)
	}

	var callEvent ToolCallEvent
	if err := json.Unmarshal([]byte(sink.events[0].Payload), &callEvent); err != nil {
		t.Fatalf("unmarshal TOOL_CALL: %v", err)
	}
	if callEvent.ToolName != "bash" || callEvent.CallID != "call_abc123" {
		t.Fatalf("TOOL_CALL event = %+v, want tool_name=bash call_id=call_abc123", callEvent)
	}

	var resultEvent ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[1].Payload), &resultEvent); err != nil {
		t.Fatalf("unmarshal TOOL_RESULT: %v", err)
	}
	if resultEvent.ToolName != "bash" || resultEvent.CallID != "call_abc123" {
		t.Fatalf("TOOL_RESULT event = %+v, want tool_name=bash call_id=call_abc123", resultEvent)
	}
	if sink.events[0].ProjectID != "project-1" || sink.events[0].TaskID.String != task.ID {
		t.Fatalf("event routing = proj %q task %q", sink.events[0].ProjectID, sink.events[0].TaskID.String)
	}
}

func assertAgenticIntegrationOutcome(t *testing.T, gw *sequenceGateway, store *mockAgenticStore) {
	t.Helper()
	if gw.callCount != 2 {
		t.Errorf("expected 2 gateway calls, got %d", gw.callCount)
	}
	if len(gw.requests) < 1 || len(gw.requests[0].Tools) == 0 {
		t.Error("first gateway request should include tool definitions")
	}
	if store.committedResult == nil || !store.committedResult.Success {
		t.Error("expected successful committed result")
	}
}

// TestAgenticLoop_MaxIterationsRespected verifies that the worker respects
// the max tool iterations limit when the gateway always returns tool calls.
// Validates: Task 07 - Worker respects max iterations
func TestAgenticLoop_MaxIterationsRespected(t *testing.T) {
	t.Parallel()
	gw, store, w, task := newMaxIterationsAgenticFixture(t)
	w.Process(context.Background(), task)
	assertMaxIterationsOutcome(t, gw, store)
}

// TestAgenticLoop_GraceFinalIterationCompletesAfterWrapUp verifies that after
// the tool-iteration cap, the worker injects a wrap-up user message and allows
// one more gateway call that can finish without further tools.
func TestAgenticLoop_GraceFinalIterationCompletesAfterWrapUp(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				Content: "Running a command.",
				ToolCalls: []gateway.ToolCall{{
					ID: "call_1", Type: "function",
					Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "echo once"}`},
				}},
			},
			{Content: "Done after grace wrap-up."},
		},
	}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"echo once": {Success: true, ExitCode: 0, Stdout: "once\n"},
	}}
	store, w, task := newAgenticIntegrationWorker(t, gw, sb, 1)
	w.Process(context.Background(), task)

	if gw.callCount != 2 {
		t.Fatalf("expected 2 gateway calls (1 tool + 1 grace), got %d", gw.callCount)
	}
	if len(gw.requests) < 2 || !requestContainsUserMessage(gw.requests[1], iterationExceededMessage) {
		t.Fatalf("grace request should include wrap-up user message, got %#v", gw.requests)
	}
	if store.committedResult == nil || !store.committedResult.Success {
		t.Fatalf("expected successful commit after grace final text, got %#v", store.committedResult)
	}
	if !strings.Contains(store.committedResult.Payload, "Done after grace wrap-up") {
		t.Fatalf("committed payload = %q, want grace final text", store.committedResult.Payload)
	}
}

func newMaxIterationsAgenticFixture(t *testing.T) (*maxIterationsGateway, *mockAgenticStore, *Worker, models.Task) {
	t.Helper()
	gw := &maxIterationsGateway{}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"echo 1": {Success: true, ExitCode: 0, Stdout: "1\n"},
		"echo 2": {Success: true, ExitCode: 0, Stdout: "2\n"},
		"echo 3": {Success: true, ExitCode: 0, Stdout: "3\n"},
	}}
	store := &mockAgenticStore{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 3})
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-max-iter"},
		ProjectID:  "project-1", AgentID: "agent-1",
		Title: "Test max iterations", State: models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace"}
	return gw, store, w, task
}

func assertMaxIterationsOutcome(t *testing.T, gw *maxIterationsGateway, store *mockAgenticStore) {
	t.Helper()
	// Three tool iterations at the cap, then one grace gateway call with the wrap-up user message.
	if gw.callCount != 4 {
		t.Errorf("expected 4 gateway calls (3 capped tool rounds + 1 grace), got %d", gw.callCount)
	}
	if len(gw.requests) < 4 {
		t.Fatalf("expected at least 4 recorded requests, got %d", len(gw.requests))
	}
	if !requestContainsUserMessage(gw.requests[3], iterationExceededMessage) {
		t.Errorf("grace request should include wrap-up user message, got %#v", gw.requests[3].Messages)
	}
	if store.task.RetryCount != 1 {
		t.Errorf("expected retry count 1, got %d", store.task.RetryCount)
	}
	if store.task.State != models.TaskStateReady {
		t.Errorf("expected READY, got %q", store.task.State)
	}
}

// TestAgenticLoop_AppendsToolResultMessages verifies that tool results are
// appended to the conversation after tool execution.
// Validates: Task 07 - First iteration executes tool, appends tool result message
func TestAgenticLoop_AppendsToolResultMessages(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "Let me run a command.", ToolCalls: []gateway.ToolCall{{
			ID: "call_test", Type: "function",
			Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "ls"}`},
		}}},
		{Content: "I see the files in the directory.", TokenUsage: 50, ProviderUsed: "openai"},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"ls": {Success: true, ExitCode: 0, Stdout: "file1.txt\nfile2.txt\n"},
	}}
	store, w, task := newAgenticIntegrationWorker(t, gw, sb, 5)
	task.ID = "task-tool-results"
	task.Title = "List files"
	w.Process(context.Background(), task)
	assertToolResultMessagesAppended(t, gw, sb, store)
}

func assertToolResultMessagesAppended(t *testing.T, gw *sequenceGateway, sb *mockAgenticSandbox, store *mockAgenticStore) {
	t.Helper()
	if gw.callCount != 2 {
		t.Errorf("expected 2 gateway calls, got %d", gw.callCount)
	}
	if sb.executionCount != 1 {
		t.Errorf("expected 1 sandbox execution, got %d", sb.executionCount)
	}
	if store.committedResult == nil {
		t.Error("expected a result to be committed")
	}
	if len(gw.requests) < 2 {
		t.Fatalf("expected at least 2 gateway requests, got %d", len(gw.requests))
	}
	for _, msg := range gw.requests[1].Messages {
		if msg.Role == "tool" {
			if !strings.Contains(msg.Content, "file1.txt") {
				t.Errorf("expected tool message with sandbox output, got %q", msg.Content)
			}
			return
		}
	}
	t.Error("expected second gateway request to include a tool-result message")
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
	if !requestContainsToolResult(mockGateway.requests[1], "call_noop", "<external_content") {
		t.Fatalf("expected second request to include wrapped noop tool result, got %#v", mockGateway.requests[1].Messages)
	}
}

// TestAgenticLoop_BudgetExceededRequeues verifies token budget enforcement through
// the full Process → processAgentic path (not only BudgetGuard unit tests).
func TestAgenticLoop_BudgetExceededRequeues(t *testing.T) {
	t.Parallel()
	gw := &tokenUsageGateway{tokensPerCall: 60}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"echo budget": {Success: true, ExitCode: 0, Stdout: "ok\n"},
	}}
	store := &mockAgenticStore{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{
		MaxToolIterations: 10,
		TokenBudget:       100,
	})
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-budget"},
		ProjectID:  "project-1", AgentID: "agent-1",
		Title: "Budget test", State: models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace"}

	w.Process(context.Background(), task)

	if gw.callCount != 2 {
		t.Fatalf("expected 2 gateway calls before budget block, got %d", gw.callCount)
	}
	if store.task.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1 (outer retry)", store.task.RetryCount)
	}
	if store.task.State != models.TaskStateReady {
		t.Fatalf("state = %q, want READY after budget failure", store.task.State)
	}
	if store.committedResult != nil {
		t.Fatal("expected no successful commit on budget exhaustion")
	}
}

// TestAgenticLoop_DeadlineExpiredBeforeIteration verifies deadline guard through Process.
func TestAgenticLoop_DeadlineExpiredBeforeIteration(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	gw := &sequenceGateway{responses: integrationSequenceResponses()}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"pwd": {Success: true, ExitCode: 0, Stdout: "/home/user\n"},
	}}
	store, w, task := newAgenticIntegrationWorker(t, gw, sb, 10)
	w.Process(ctx, task)

	if gw.callCount != 0 {
		t.Fatalf("expected no gateway calls when deadline already expired, got %d", gw.callCount)
	}
	if store.task.RetryCount != 1 {
		t.Fatalf("RetryCount = %d, want 1", store.task.RetryCount)
	}
	if store.task.State != models.TaskStateReady {
		t.Fatalf("state = %q, want READY", store.task.State)
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
	if !requestContainsToolResult(mockGateway.requests[1], "call_fail", "boom") {
		t.Fatalf("expected second request to include tool stderr in context, got %#v", mockGateway.requests[1].Messages)
	}
	if store.committedResult == nil || !store.committedResult.Success {
		t.Fatalf("expected final response committed after tool error, got %#v", store.committedResult)
	}
}
