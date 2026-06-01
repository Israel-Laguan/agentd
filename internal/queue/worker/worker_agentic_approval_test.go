package worker

import (
	"context"
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
	taskHooks.RegisterPre(ApprovalGateHook([]string{"deploy"}, handler))
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
