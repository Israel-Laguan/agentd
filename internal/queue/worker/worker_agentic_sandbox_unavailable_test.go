package worker

import (
	"context"
	"os"
	"testing"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

func bashCall(id, command string) gateway.AIResponse {
	return gateway.AIResponse{Content: "run", ToolCalls: []gateway.ToolCall{{
		ID: id, Type: "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"` + command + `"}`},
	}}}
}

// runAgenticTask drives one agentic task the way processRunningTask does and
// returns the worker, sink and running task.
func runAgenticTask(t *testing.T, gw gateway.AIGateway, sb sandbox.Executor) (*Worker, *mockEventSink, models.Task) {
	t.Helper()
	return runAgenticTaskIn(t, gw, sb, nil)
}

// runAgenticTaskIn is runAgenticTask with a hook that can seed the workspace
// (for example with subagent definitions) before the loop starts.
func runAgenticTaskIn(t *testing.T, gw gateway.AIGateway, sb sandbox.Executor, prep func(workspace string)) (*Worker, *mockEventSink, models.Task) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(t.TempDir())
	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "host", Description: "d",
		Tasks: []models.DraftTask{{Title: "Inspect host", Description: testutil.AgenticTestTaskDescription()}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if err := os.MkdirAll(proj.WorkspacePath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if prep != nil {
		prep(proj.WorkspacePath)
	}
	profile := models.AgentProfile{ID: tasks[0].AgentID, Provider: "openai", Model: "gpt-4", AgenticMode: true}
	if err := store.UpsertAgentProfile(ctx, profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	sink := &mockEventSink{}
	w := NewWorker(store, gw, sb, nil, sink, WorkerOptions{MaxToolIterations: 5})
	if result, ok := w.processAgentic(ctx, *running, *proj, profile); ok {
		w.handleLoopResult(ctx, *running, result)
	}
	return w, sink, *running
}

func assertAgenticRefusedTerminal(t *testing.T, w *Worker, taskID string) {
	t.Helper()
	got, err := w.store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailed || got.RetryCount != 0 {
		t.Fatalf("task state = %s retries = %d, want FAILED with no retry: a missing required jail is terminal", got.State, got.RetryCount)
	}
}

// B-052: in agentic mode a bash call refused by an unavailable required jail
// ends the task terminally after one model turn, instead of requeueing it.
func TestAgenticBashFailsTerminalWhenSandboxUnavailable(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{bashCall("c1", "uname -a"), {Content: "done"}}}
	sb := &scriptedSandbox{steps: []scriptedStep{{result: sandbox.Result{ExitCode: -1}, err: refusedWithoutJail()}}}
	w, _, task := runAgenticTask(t, gw, sb)
	assertAgenticRefusedTerminal(t, w, task.ID)
	if gw.callCount != 1 {
		t.Fatalf("gateway calls = %d, want 1: the loop must stop at the refusal", gw.callCount)
	}
}

// The privilege-recovery alternative (RecoverAgenticPrivilege) runs a second
// command; if the jail refuses it, the task still fails terminally.
func TestAgenticPrivilegeAlternativeFailsTerminalWhenSandboxUnavailable(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "sudo dmidecode -t memory"), {Content: `{"command":"free -h"}`}, {Content: "done"},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: sandbox.Result{ExitCode: -1, Stderr: "sudo command blocked"}},
		{result: sandbox.Result{ExitCode: -1}, err: refusedWithoutJail()},
	}}
	w, _, task := runAgenticTask(t, gw, sb)
	assertAgenticRefusedTerminal(t, w, task.ID)
}

func TestHandleLoopResultToolFailureRouting(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		code      string
		wantState models.TaskState
	}{
		{"sandbox unavailable is terminal", agenttools.ToolErrorCodeSandboxUnavailable, models.TaskStateFailed},
		{"other tool failure keeps retry path", "", models.TaskStateReady},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, w, _, task, _ := newCommandEventWorker(t, &scriptedSandbox{})
			running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
			if err != nil {
				t.Fatalf("MarkTaskRunning: %v", err)
			}
			w.handleLoopResult(ctx, *running, LoopResult{Status: LoopToolFailure, Meta: LoopMeta{ToolName: "bash", LastError: "refused", ErrorCode: tt.code}})
			got, _ := w.store.GetTask(ctx, task.ID)
			if got.State != tt.wantState {
				t.Fatalf("state = %s, want %s", got.State, tt.wantState)
			}
		})
	}
}
