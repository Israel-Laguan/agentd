package worker

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// B-082: the builtin agent reported COMPLETED after its own verification had
// failed (a test it wrote fails, or the toolchain was missing). A finish with
// no tool call right after a failed shell command must not be COMPLETED.
func TestAgenticFinishAfterFailedCommandHandsTaskToHuman(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "go test ./..."), {Content: "Done. The feature is complete."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: sandbox.Result{ExitCode: 1, Stderr: "--- FAIL: TestCountLines"}},
	}}
	w, _, task := runAgenticTask(t, gw, sb)
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN: the last command failed and the task finished without a passing run", got.State)
	}
}

// A failed command followed by a passing one is verified: the task completes.
func TestAgenticFinishAfterPassingRerunCompletes(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "go test ./..."), bashCall("c2", "go test ./..."), {Content: "Done. All tests pass."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: sandbox.Result{ExitCode: 1, Stderr: "--- FAIL: TestCountLines"}},
		{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}},
	}}
	w, _, task := runAgenticTask(t, gw, sb)
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED after a passing rerun", got.State)
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

	var recorded LoopResult
	w.SetLoopResultRecorder(func(r LoopResult) { recorded = r })
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
	// B-082: a final answer given after a failed command with nothing passing
	// after it is not committed as a result; the task goes to a human.
	if store.committedResult != nil && store.committedResult.Success {
		t.Fatalf("final response after a failed command must not be committed as success, got %#v", store.committedResult)
	}
	if recorded.Status != LoopToolFailure || recorded.Meta.ErrorCode != agenttools.ToolErrorCodeUnverified {
		t.Fatalf("loop result = %s code %q, want tool_failure with %q", recorded.Status, recorded.Meta.ErrorCode, agenttools.ToolErrorCodeUnverified)
	}
}

// The agentic default prompt is prose with no JSON contract. When the routed
// provider cannot do tool calls the engine falls back to the legacy JSON path,
// which decodes a workerResponse; the profile prompt alone then gives the model
// nothing to answer in (B-080 review).
func TestLegacySystemContentAddsTheJSONContractToAProseOnlyPrompt(t *testing.T) {
	t.Parallel()
	prose := models.AgentProfile{SystemPrompt: sql.NullString{String: "Do the requested task in the workspace with the tools you have.", Valid: true}}
	got := legacyJSONCommandSystemContent(prose)
	if !strings.HasPrefix(got, prose.SystemPrompt.String) {
		t.Errorf("profile prompt must come first, got %q", got)
	}
	if !strings.Contains(got, `{"command":`) {
		t.Errorf("content = %q; want the JSON command contract appended to a prose-only prompt", got)
	}
	json := models.AgentProfile{SystemPrompt: sql.NullString{String: "Suggest one safe shell command. Output only JSON.", Valid: true}}
	if got := legacyJSONCommandSystemContent(json); got != json.SystemPrompt.String {
		t.Errorf("a prompt that already asks for JSON must be used as it is, got %q", got)
	}
}
