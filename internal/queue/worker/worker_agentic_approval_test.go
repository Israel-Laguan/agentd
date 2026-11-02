package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestHandleAgenticToolCalls_ResumesAfterApproval(t *testing.T) {
	t.Parallel()
	store, w, parent, resp, ex, taskHooks, _ := setupApprovalResumeFixture(t)
	ctx := context.Background()

	_, suspended := w.DispatchToolWithHooks(ctx, parent.ID, parent.ProjectID, "", parent.UpdatedAt, resp.ToolCalls[0], nil, ex, taskHooks, nil, "")
	if !suspended {
		t.Fatal("expected approval gate to suspend on first tool call")
	}
	if !store.blockCalled {
		t.Fatal("expected BlockTaskWithSubtasks on first gated tool call")
	}
	completeApprovalSubtask(t, store, ctx, parent.ID)

	parentAfter, err := store.GetTask(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get parent after approval: %v", err)
	}
	if parentAfter.State != models.TaskStateReady {
		t.Fatalf("parent state = %s, want READY after approval", parentAfter.State)
	}

	store.blockCalled = false
	tr, suspended := w.DispatchToolWithHooks(ctx, parentAfter.ID, parentAfter.ProjectID, "", parentAfter.UpdatedAt, resp.ToolCalls[0], nil, ex, taskHooks, nil, "")
	if suspended {
		t.Fatal("expected tool to proceed after human approval, not suspend again")
	}
	assertApprovalResumed(t, store, tr)
}

func setupApprovalResumeFixture(t *testing.T) (
	*approvalMockStore, *Worker, models.Task, gateway.AIResponse, *agenttools.ToolExecutor, *agenthooks.HookChain, *agentcontext.ContextManager,
) {
	t.Helper()
	store := &approvalMockStore{FakeKanbanStore: testutil.NewFakeStore()}
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p", Tasks: []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]
	handler := NewBlockingApprovalHandler(store)
	taskHooks := agenthooks.NewHookChain()
	taskHooks.RegisterPre(ApprovalGateHook([]string{"deploy"}, handler, 0))
	ex := agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0)
	resp := gateway.AIResponse{ToolCalls: []gateway.ToolCall{{
		ID: "call-1", Function: gateway.ToolCallFunction{Name: "deploy", Arguments: `{}`},
	}}}
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{}, nil, "agent", parent.ID)
	return store, &Worker{store: store}, parent, resp, ex, taskHooks, cm
}

func completeApprovalSubtask(t *testing.T, store *approvalMockStore, ctx context.Context, parentID string) {
	t.Helper()
	children, err := store.ListChildTasks(ctx, parentID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("approval subtasks = %d, want 1", len(children))
	}
	if _, err := store.UpdateTaskState(ctx, children[0].ID, children[0].UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete approval subtask: %v", err)
	}
}

func assertApprovalResumed(t *testing.T, store *approvalMockStore, tr agenttools.ToolResult) {
	t.Helper()
	if store.blockCalled {
		t.Fatal("expected no second BlockTaskWithSubtasks after completed approval")
	}
	if strings.Contains(tr.Content, "paused pending human approval") {
		t.Fatalf("tool result should not re-block: %q", tr.Content)
	}
}

// B-089 runs before the approval gate: a whole-file write to a file whose read
// was cut is refused as a tool error, so no human is asked to approve a write
// that would be refused anyway.
func TestGatedWriteToACutFileIsRefusedWithoutApproval(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100000)
	call := func(id, name, args string) gateway.AIResponse {
		return gateway.AIResponse{ToolCalls: []gateway.ToolCall{{ID: id, Type: "function", Function: gateway.ToolCallFunction{Name: name, Arguments: args}}}}
	}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		call("r", "read", `{"path":"big.txt"}`), call("w", "write", `{"path":"big.txt","content":"short"}`), {Content: "Done."},
	}}
	var workspace string
	w, _, task, project, profile := agenticFixtureWith(t, gw, &batchCountingSandbox{}, models.DraftTask{Title: "t", Description: testutil.AgenticTestTaskDescription()},
		func(ws string) {
			workspace = ws
			if err := os.WriteFile(filepath.Join(ws, "big.txt"), []byte(long), 0o644); err != nil {
				t.Fatal(err)
			}
		}, WorkerOptions{MaxToolIterations: 5, AgenticCharacterBudget: 40000})
	profile.GatedTools = []string{"write"}
	ctx := context.Background()
	if result, ok := w.processAgentic(ctx, task, project, profile); ok {
		w.handleLoopResult(ctx, task, result)
	}

	if len(gw.requests) < 2 || strings.Contains(toolMessage(gw.requests[1], "r"), long) {
		t.Fatal("the read reached the model whole; the test would prove nothing")
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	for _, c := range children {
		if strings.HasPrefix(c.Title, models.HITLSubtaskTitleApproveTool) {
			t.Fatalf("approval subtask %q created for a write that is refused anyway", c.Title)
		}
	}
	if got, _ := os.ReadFile(filepath.Join(workspace, "big.txt")); string(got) != long {
		t.Fatal("big.txt changed; the refused write must not run")
	}
	if len(gw.requests) < 3 || !strings.Contains(toolMessage(gw.requests[2], "w"), "write refused") {
		t.Fatalf("model turns = %d; want the B-089 refusal shown to the model on the next turn", len(gw.requests))
	}
}

func toolMessage(req gateway.AIRequest, callID string) string {
	for _, m := range req.Messages {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m.Content
		}
	}
	return ""
}
