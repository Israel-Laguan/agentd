package subagent

import (
	"context"
	"testing"

	"agentd/internal/gateway"
)

// T-057: the subagent's model turns take their task from ctx, which the worker
// sets when a task's tool executor delegates.
func TestSubagentTurnsCarryTheTaskIDFromContext(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{}
	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0)
	ctx := gateway.WithTaskID(context.Background(), "task-42")

	if _, err := delegate.Delegate(ctx, SubagentDefinition{Name: "helper", Purpose: "help"}, "do work", "", "", 0.2, 0); err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if len(gw.requests) == 0 {
		t.Fatal("no model call reached the gateway")
	}
	for i, req := range gw.requests {
		if req.TaskID != "task-42" {
			t.Errorf("model call %d: TaskID = %q, want %q", i, req.TaskID, "task-42")
		}
	}
}

// The delegating task's attributes reach every subagent turn, so a routing rule
// on overhead.subagent_turn can match it.
func TestSubagentTurnsCarryTheDelegatingTasksAttrs(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{}
	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0).WithAttrs(gateway.AttrsForTask(true))

	if _, err := delegate.Delegate(context.Background(), SubagentDefinition{Name: "helper", Purpose: "help"}, "do work", "", "", 0.2, 0); err != nil {
		t.Fatalf("Delegate: %v", err)
	}
	if len(gw.requests) == 0 {
		t.Fatal("no model call reached the gateway")
	}
	for i, req := range gw.requests {
		if req.Attrs.RequiresWorkspaceChange == nil || !*req.Attrs.RequiresWorkspaceChange {
			t.Errorf("model call %d: Attrs = %+v, want requires_workspace_change=true", i, req.Attrs)
		}
	}
	if child := delegate.child(); child.attrs.RequiresWorkspaceChange == nil || !*child.attrs.RequiresWorkspaceChange {
		t.Error("a nested delegation dropped the attributes")
	}
}
