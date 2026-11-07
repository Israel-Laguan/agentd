package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/agent/acp"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// acpPermission answers one ACP bash permission request for a task run under
// profile, through the hooks MountAgenticHooks builds for it. The command is a
// write, which the command policy leaves to the hooks.
func acpPermission(t *testing.T, profile models.AgentProfile) (acp.Verdict, *testutil.FakeKanbanStore, models.Task) {
	t.Helper()
	return acpCommandPermission(t, profile, "touch notes.txt")
}

// acpCommandPermission is acpPermission for command.
func acpCommandPermission(t *testing.T, profile models.AgentProfile, command string) (acp.Verdict, *testutil.FakeKanbanStore, models.Task) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "p", Tasks: []models.DraftTask{{Title: "t", Description: "d"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	w := NewWorker(store, nil, &batchCountingSandbox{}, nil, &mockEventSink{}, WorkerOptions{})
	taskHooks, _ := w.MountAgenticHooks(models.Project{}, profile)
	args, _ := json.Marshal(map[string]string{"command": command})
	verdict, err := ACPPermitter(tasks[0], taskHooks)(ctx, acp.Permission{ToolCallID: "call-1", Tool: "bash", Args: string(args), Command: command})
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

// The command policy runs before the task's hooks (T-062): a denied command
// is refused even though bash is gated, an allowed one never reaches the gate,
// and anything else suspends for approval as before.
func TestACPCommandPolicyRunsBeforeTheGate(t *testing.T) {
	t.Parallel()
	gated := models.AgentProfile{GatedTools: []string{"bash"}}
	cases := []struct {
		command string
		want    acp.Verdict
	}{
		{command: "rm -rf /", want: acp.Verdict{Reason: `command blocked: contains dangerous pattern "rm -rf /"`}},
		{command: "go test ./...", want: acp.Verdict{Allow: true}},
		{command: "touch notes.txt", want: acp.Verdict{Suspend: true}},
	}
	for _, tc := range cases {
		v, store, task := acpCommandPermission(t, gated, tc.command)
		children, _ := store.ListChildTasks(context.Background(), task.ID)
		if v.Allow != tc.want.Allow || v.Suspend != tc.want.Suspend || (tc.want.Reason != "" && v.Reason != tc.want.Reason) {
			t.Errorf("%q: verdict = %+v; want %+v", tc.command, v, tc.want)
		}
		if wantChildren := map[bool]int{true: 1}[tc.want.Suspend]; len(children) != wantChildren {
			t.Errorf("%q: subtasks = %d; want %d", tc.command, len(children), wantChildren)
		}
	}
}
