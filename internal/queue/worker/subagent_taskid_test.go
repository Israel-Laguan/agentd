package worker

import (
	"context"
	"testing"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
)

// T-057: a subagent turn carries the task whose tool executor delegated it,
// so the ai_calls row for it shows up in that task's per-task view.
func TestWorker_DelegateTurnsCarryTheTaskID(t *testing.T) {
	t.Parallel()

	dir := writeSubagentDefinition(t, "helper", "# Subagent: helper\n\n## Purpose\n\nHelp with a bounded task.\n")
	gw := &subagentMockGateway{}
	w := &Worker{gateway: gw}
	toolExec := agenttools.NewToolExecutor(nil, dir, nil, 0).ForTask("task-42", "proj-1")

	w.DispatchTool(context.Background(), "session", gateway.ToolCall{
		ID:   "delegate-call",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      agenttools.ToolNameDelegate,
			Arguments: `{"subagent":"helper","task":"do helper work"}`,
		},
	}, nil, toolExec)

	requireTaskIDs(t, gw.requestSnapshot(), "task-42")
}

// T-057: the parallel delegate path carries the task the same way.
func TestWorker_DelegateParallelTurnsCarryTheTaskID(t *testing.T) {
	t.Parallel()

	dir := writeSubagentDefinition(t, "helper", "# Subagent: helper\n\n## Purpose\n\nHelp with a bounded task.\n")
	gw := &subagentMockGateway{}
	w := &Worker{gateway: gw}
	toolExec := agenttools.NewToolExecutor(nil, dir, nil, 0).ForTask("task-42", "proj-1")

	w.DispatchTool(context.Background(), "session", gateway.ToolCall{
		ID:   "parallel-call",
		Type: "function",
		Function: gateway.ToolCallFunction{
			Name:      agenttools.ToolNameDelegateParallel,
			Arguments: `{"tasks":[{"subagent":"helper","task":"do helper work"}]}`,
		},
	}, nil, toolExec)

	requireTaskIDs(t, gw.requestSnapshot(), "task-42")
}

// requireTaskIDs fails unless at least one model call reached the gateway and
// every call carries want as its TaskID.
func requireTaskIDs(t *testing.T, reqs []gateway.AIRequest, want string) {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("no model call reached the gateway")
	}
	for i, req := range reqs {
		if req.TaskID != want {
			t.Errorf("model call %d: TaskID = %q, want %q", i, req.TaskID, want)
		}
	}
}
