package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/agent/acp"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// acpPermission answers one ACP bash permission request for a task run under
// profile, through the hooks MountAgenticHooks builds for it.
func acpPermission(t *testing.T, profile models.AgentProfile) (acp.Verdict, *testutil.FakeKanbanStore, models.Task) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "p", Tasks: []models.DraftTask{{Title: "t", Description: "d"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	w := NewWorker(store, nil, &batchCountingSandbox{}, nil, &mockEventSink{}, WorkerOptions{})
	taskHooks, _ := w.MountAgenticHooks(models.Project{}, profile)
	verdict, err := ACPPermitter(tasks[0], taskHooks)(ctx, acp.Permission{ToolCallID: "call-1", Tool: "bash", Args: `{"command":"go test ./..."}`, Command: "go test ./..."})
	if err != nil {
		t.Fatalf("permit: %v", err)
	}
	return verdict, store, tasks[0]
}

func TestACPPermissionWithNoTaskHooksIsAllowed(t *testing.T) {
	t.Parallel()
	if v, _, _ := acpPermission(t, models.AgentProfile{}); !v.Allow || v.Suspend {
		t.Fatalf("verdict = %+v; want allowed", v)
	}
}

func TestACPPermissionOutsideAllowedToolsIsDenied(t *testing.T) {
	t.Parallel()
	v, store, task := acpPermission(t, models.AgentProfile{AllowedTools: []string{"read"}})
	if v.Allow || v.Suspend || !strings.Contains(v.Reason, "allowed_tools") {
		t.Fatalf("verdict = %+v; want a denial naming allowed_tools", v)
	}
	if children, _ := store.ListChildTasks(context.Background(), task.ID); len(children) != 0 {
		t.Fatalf("subtasks = %+v; an unlisted tool never asks a human", children)
	}
}

// A gated tool stops for a HUMAN approval subtask, the same HITL path as the
// builtin agent's call, and the verdict suspends the ACP call.
func TestACPPermissionForAGatedToolStopsForHumanApproval(t *testing.T) {
	t.Parallel()
	v, store, task := acpPermission(t, models.AgentProfile{GatedTools: []string{"bash"}})
	if v.Allow || !v.Suspend {
		t.Fatalf("verdict = %+v; want the call suspended for approval", v)
	}
	children, err := store.ListChildTasks(context.Background(), task.ID)
	if err != nil || len(children) != 1 || children[0].Title != models.HITLSubtaskTitleApproveTool+"bash" || children[0].Assignee != models.TaskAssigneeHuman {
		t.Fatalf("subtasks = %+v, err = %v; want one HUMAN approval subtask for bash", children, err)
	}
}
