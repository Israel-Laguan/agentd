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
