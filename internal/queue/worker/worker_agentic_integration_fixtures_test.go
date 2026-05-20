package worker

import (
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

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
		BaseEntity:  models.BaseEntity{ID: "task-integration-test"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Check current directory",
		Description: testutil.AgenticTestTaskDescription(),
		State:       models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace"}
	store.task = task
	return store, w, task
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
		BaseEntity:  models.BaseEntity{ID: "task-max-iter"},
		ProjectID:   "project-1",
		AgentID:     "agent-1",
		Title:       "Test max iterations",
		Description: testutil.AgenticTestTaskDescription(),
		State:       models.TaskStateQueued,
	}
	store.profile = models.AgentProfile{ID: "agent-1", Provider: "openai", Model: "gpt-4", AgenticMode: true}
	store.project = models.Project{BaseEntity: models.BaseEntity{ID: "project-1"}, WorkspacePath: "/tmp/test-workspace"}
	store.task = task
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
