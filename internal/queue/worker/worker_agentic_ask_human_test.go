package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agenthooks "agentd/internal/agent/hooks"
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

// The question is recorded on the task, so the run that resumes after the
// answer sees it. Without it the resumed run sees only "Clarification".
func TestAskHumanRecordsQuestionForResumedRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "ask-proj",
		Tasks:       []models.DraftTask{{Title: "Report", Description: "write the report"}},
	})
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	w := &Worker{store: store}
	if err := w.AskHuman(ctx, *running, "Which output format?", []string{"CSV", "JSON"}); err != nil {
		t.Fatalf("AskHuman: %v", err)
	}
	children, err := store.ListChildTasks(ctx, running.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("children = %d (err %v), want one clarification subtask", len(children), err)
	}
	if err := store.AddComment(ctx, models.Comment{TaskID: children[0].ID, Author: models.CommentAuthorUser, Body: "CSV"}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, children[0].ID, children[0].UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete child: %v", err)
	}
	current, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	resumed, consumed, err := w.tryConsumeElicitationAnswers(ctx, *current)
	if err != nil || !consumed {
		t.Fatalf("tryConsumeElicitationAnswers consumed=%v err=%v, want the answer consumed", consumed, err)
	}
	for _, want := range []string{"Which output format?", "CSV"} {
		if !strings.Contains(resumed.Description, want) {
			t.Fatalf("resumed description = %q, want it to hold %q", resumed.Description, want)
		}
	}
}

// The run that resumes after the human answers must show the model the answer.
// Elicitation adds it to the description after the prompt is built, so the
// prompt has to be rebuilt from the clarified task.
func TestAgenticResumedRunPromptCarriesTheAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		askHumanCall("c1", "Which output format?"), {Content: "Wrote the CSV report."},
	}}
	w, _, task, project, profile := agenticFixture(t, gw, &scriptedSandbox{}, models.DraftTask{Title: "Write the report", Description: testutil.AgenticTestTaskDescription()}, nil)
	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil || len(children) != 1 {
		t.Fatalf("children = %d (err %v), want one clarification subtask", len(children), err)
	}
	if err := w.store.AddComment(ctx, models.Comment{TaskID: children[0].ID, Author: models.CommentAuthorUser, Body: "CSV-ANSWER"}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := w.store.UpdateTaskState(ctx, children[0].ID, children[0].UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete child: %v", err)
	}
	current, err := w.store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}

	w.processAgentic(ctx, *current, project, profile)

	if len(gw.requests) < 2 {
		t.Fatalf("gateway requests = %d, want the resumed run to call the model", len(gw.requests))
	}
	for _, msg := range gw.requests[1].Messages {
		if msg.Section == gateway.SectionTask && strings.Contains(msg.Content, "CSV-ANSWER") {
			return
		}
	}
	t.Fatalf("resumed run's task message lacks the human's answer: %#v", gw.requests[1].Messages)
}

// ask_human goes through tool dispatch like any other tool: a pre-hook can veto
// it, and a post-hook (the dispatch audit among them) sees the call.
func TestAgenticAskHumanRunsThroughDispatchHooks(t *testing.T) {
	t.Parallel()
	for _, veto := range []bool{true, false} {
		gw := &sequenceGateway{responses: []gateway.AIResponse{
			askHumanCall("c1", "Which format?"), {Content: "Done."},
		}}
		w, _, task, project, profile := agenticFixture(t, gw, &scriptedSandbox{}, models.DraftTask{Title: "Write the report", Description: testutil.AgenticTestTaskDescription()}, nil)
		var posted []string
		w.hooks = agenthooks.NewHookChain()
		w.hooks.RegisterPre(agenthooks.PreHook{Name: "gate", Policy: agenthooks.FailClosed, Fn: func(ctx agenthooks.HookContext) (agenthooks.HookVerdict, error) {
			if veto && ctx.ToolName == agenttools.ToolNameAskHuman {
				return agenthooks.HookVerdict{Veto: true, Reason: "humans are off today"}, nil
			}
			return agenthooks.HookVerdict{}, nil
		}})
		w.hooks.RegisterPost(agenthooks.PostHook{Name: "seen", Policy: agenthooks.FailOpen, Fn: func(ctx agenthooks.HookContext, result string) (string, error) {
			posted = append(posted, ctx.ToolName)
			return result, nil
		}})
		if result, ok := w.processAgentic(context.Background(), task, project, profile); ok {
			w.handleLoopResult(context.Background(), task, result)
		}
		got, err := w.store.GetTask(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("GetTask: %v", err)
		}
		if blocked := got.State == models.TaskStateBlocked; blocked == veto {
			t.Fatalf("veto=%v: state = %s, want blocked only without the veto", veto, got.State)
		}
		if len(posted) != 1 || posted[0] != agenttools.ToolNameAskHuman {
			t.Fatalf("veto=%v: post-hooks saw %v, want the ask_human call once", veto, posted)
		}
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
	if len(gw.requests) < 2 || !requestContainsToolResult(gw.requests[1], "c1", "not in this profile's allowed_tools") {
		t.Fatalf("ask_human was not refused by the allowlist veto: requests = %d", len(gw.requests))
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
