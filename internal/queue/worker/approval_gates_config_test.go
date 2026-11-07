package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// gatedBash runs one bash call, through the hook chain a worker built with
// gates mounts for profile, on a task in a fake store. It reports whether the
// call stopped for approval, how many times the sandbox ran, and the store.
// The command is a write, which the command policy leaves to the gate.
func gatedBash(t *testing.T, gates config.ApprovalGatesConfig, profile models.AgentProfile) (bool, int, *testutil.FakeKanbanStore, models.Task) {
	t.Helper()
	suspended, calls, store, task, _ := gatedCommand(t, gates, profile, "touch notes.txt")
	return suspended, calls, store, task
}

// gatedCommand is gatedBash for command, and also returns the call's result.
func gatedCommand(t *testing.T, gates config.ApprovalGatesConfig, profile models.AgentProfile, command string) (bool, int, *testutil.FakeKanbanStore, models.Task, agenttools.ToolResult) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "p", Tasks: []models.DraftTask{{Title: "t", Description: "d"}}})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	sb := &batchCountingSandbox{}
	w := NewWorker(store, nil, sb, nil, &mockEventSink{}, WorkerOptions{ApprovalGates: gates})
	taskHooks, _ := w.MountAgenticHooks(models.Project{}, profile)
	executor := agenttools.NewToolExecutor(sb, t.TempDir(), agenttools.BuildSandboxEnv(nil, nil), 0)
	args, _ := json.Marshal(map[string]string{"command": command})
	call := gateway.ToolCall{ID: "call_bash", Function: gateway.ToolCallFunction{Name: "bash", Arguments: string(args)}}
	tr, suspended := w.DispatchToolWithHooks(ctx, tasks[0].ID, "", "turn-1", tasks[0].UpdatedAt,
		call, nil, executor, taskHooks, nil, profile.Provider)
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return suspended, sb.calls, store, tasks[0], tr
}

// A profile's gated_tools gate its calls whatever agentic.approval_gates says;
// enabled switches only the global list.
func TestProfileGatedToolsGateWithApprovalGatesOff(t *testing.T) {
	t.Parallel()
	suspended, calls, _, _ := gatedBash(t, config.ApprovalGatesConfig{}, models.AgentProfile{GatedTools: []string{"bash"}})
	if !suspended || calls != 0 {
		t.Fatalf("suspended = %t, sandbox calls = %d; want the call stopped for approval before it runs", suspended, calls)
	}
}

func TestApprovalGatesToolsGateEveryProfileWhenEnabled(t *testing.T) {
	t.Parallel()
	suspended, calls, _, _ := gatedBash(t, config.ApprovalGatesConfig{Enabled: true, Tools: []string{"bash"}}, models.AgentProfile{})
	if !suspended || calls != 0 {
		t.Fatalf("suspended = %t, sandbox calls = %d; want the call stopped for approval before it runs", suspended, calls)
	}
}

func TestApprovalGatesToolsGateNothingWhenDisabled(t *testing.T) {
	t.Parallel()
	suspended, calls, _, _ := gatedBash(t, config.ApprovalGatesConfig{Tools: []string{"bash"}}, models.AgentProfile{})
	if suspended || calls != 1 {
		t.Fatalf("suspended = %t, sandbox calls = %d; want the call to run: approval_gates is off", suspended, calls)
	}
}

// A gate name matches ignoring case, so a gate written "BASH" still stops bash.
func TestGatedToolNameMatchesIgnoringCase(t *testing.T) {
	t.Parallel()
	suspended, calls, _, _ := gatedBash(t, config.ApprovalGatesConfig{}, models.AgentProfile{GatedTools: []string{"BASH"}})
	if !suspended || calls != 0 {
		t.Fatalf("suspended = %t, sandbox calls = %d; want BASH to gate bash", suspended, calls)
	}
}

// agentic.approval_gates.timeout sets the approval's deadline, after which the
// HITL reconciler fails the task. Left unset, the deadline is
// DefaultApprovalTimeout, never an open-ended wait.
func TestApprovalGatesTimeoutSetsTheApprovalDeadline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		timeout, want time.Duration
	}{{2 * time.Hour, 2 * time.Hour}, {0, DefaultApprovalTimeout}} {
		before := time.Now()
		_, _, store, task := gatedBash(t, config.ApprovalGatesConfig{Timeout: tc.timeout}, models.AgentProfile{GatedTools: []string{"bash"}})
		comments, err := store.ListComments(context.Background(), task.ID)
		if err != nil {
			t.Fatalf("ListComments: %v", err)
		}
		expiresAt, ok := parseHITLExpiry(comments)
		if !ok {
			t.Fatalf("timeout %v: no approval deadline recorded", tc.timeout)
		}
		// The deadline is stored to the second.
		if expiresAt.Before(before.Add(tc.want).Truncate(time.Second)) || expiresAt.After(time.Now().Add(tc.want)) {
			t.Fatalf("timeout %v: deadline = %v, want %v after the call (%v)", tc.timeout, expiresAt, tc.want, before)
		}
	}
}

// The command policy runs before the gate on the builtin path (T-062): a
// denied command is refused even though bash is gated, an allowed one runs
// unasked, and anything else still stops for approval.
func TestCommandPolicyRunsBeforeTheBuiltinGate(t *testing.T) {
	t.Parallel()
	gated := models.AgentProfile{GatedTools: []string{"bash"}}
	cases := []struct {
		command   string
		suspended bool
		calls     int
		vetoed    bool
	}{
		{command: "rm -rf /", vetoed: true},
		{command: "go test ./...", calls: 1},
		{command: "touch notes.txt", suspended: true},
	}
	for _, tc := range cases {
		suspended, calls, store, task, tr := gatedCommand(t, config.ApprovalGatesConfig{}, gated, tc.command)
		children, _ := store.ListChildTasks(context.Background(), task.ID)
		if suspended != tc.suspended || calls != tc.calls || len(children) != map[bool]int{true: 1}[tc.suspended] {
			t.Errorf("%q: suspended = %t, sandbox calls = %d, subtasks = %d; want %t, %d", tc.command, suspended, calls, len(children), tc.suspended, tc.calls)
		}
		if vetoed := tr.Status == agenttools.ToolStatusVetoed && strings.Contains(tr.Content, "dangerous pattern"); vetoed != tc.vetoed {
			t.Errorf("%q: result = %+v; want vetoed by the policy: %t", tc.command, tr, tc.vetoed)
		}
	}
}
