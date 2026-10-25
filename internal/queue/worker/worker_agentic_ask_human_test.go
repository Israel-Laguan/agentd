package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// askHumanCall is a model turn that calls the ask_human builtin with question.
func askHumanCall(id, question string) gateway.AIResponse {
	args, _ := json.Marshal(map[string]string{"question": question})
	return gateway.AIResponse{Content: "I need an answer before I continue.", ToolCalls: []gateway.ToolCall{{
		ID: id, Type: "function",
		Function: gateway.ToolCallFunction{Name: "ask_human", Arguments: string(args)},
	}}}
}

// B-082 (ask_human part): the agentic loop had no way to ask a human, so an
// ambiguous task was guessed at and reported COMPLETED. A scripted ask_human
// call blocks the task and creates a clarification subtask that holds the
// question, and the loop stops at the call instead of asking the model again.
func TestAgenticAskHumanBlocksTaskWithClarification(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		askHumanCall("c1", "Which output format did the client ask for?"), {Content: "Done."},
	}}
	w, _, task := runAgenticTask(t, gw, &scriptedSandbox{})
	ctx := context.Background()

	got, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED: ask_human must hand the task to a human", got.State)
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if len(children) != 1 || !strings.HasPrefix(children[0].Title, models.HITLSubtaskTitleClarification) {
		t.Fatalf("children = %#v, want one clarification subtask", children)
	}
	if !strings.Contains(children[0].Description, "Which output format did the client ask for?") {
		t.Fatalf("clarification description = %q, want the question", children[0].Description)
	}
	if gw.callCount != 1 {
		t.Fatalf("gateway calls = %d, want 1: the loop must stop at the ask_human call", gw.callCount)
	}
}

// A profile whose AllowedTools leave out ask_human never blocks the task on it:
// the call goes to dispatch, whose allowlist veto refuses it.
func TestAgenticAskHumanRefusedByAllowlist(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		askHumanCall("c1", "Which format?"), {Content: "Done."},
	}}
	w, _, task, project, profile := agenticFixture(t, gw, &scriptedSandbox{}, models.DraftTask{Title: "Inspect host", Description: testutil.AgenticTestTaskDescription()}, nil)
	profile.AllowedTools = []string{"bash"}
	if result, ok := w.processAgentic(context.Background(), task, project, profile); ok {
		w.handleLoopResult(context.Background(), task, result)
	}
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State == models.TaskStateBlocked {
		t.Fatalf("state = BLOCKED, want the allowlist to refuse ask_human")
	}
}

// ask_human is offered by the worker, never by the executor: a subagent builds
// its tools from the executor's definitions and cannot suspend a task.
func TestAskHumanIsNotAnExecutorTool(t *testing.T) {
	t.Parallel()
	executor := agenttools.NewToolExecutor(nil, "", nil, 0)
	if containsTool(executor.Definitions(), agenttools.ToolNameAskHuman) {
		t.Fatalf("executor definitions include ask_human; subagents would inherit it")
	}
}
