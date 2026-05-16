package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
)

// ---------------------------------------------------------------------------
// SubagentDelegate — parallel delegation
// ---------------------------------------------------------------------------

func TestSubagentDelegate_Parallel(t *testing.T) {
	t.Parallel()

	gw := &subagentMockGateway{
		responses: []gateway.AIResponse{
			{Content: "result 1"},
			{Content: "result 2"},
		},
	}

	tasks := []ParallelTask{
		{
			Definition:  SubagentDefinition{Name: "a", Purpose: "first"},
			Description: "task a",
		},
		{
			Definition:  SubagentDefinition{Name: "b", Purpose: "second"},
			Description: "task b",
		},
	}

	delegate := NewSubagentDelegate(gw, nil, t.TempDir(), nil, 0, 0)
	results := delegate.DelegateParallel(context.Background(), tasks, "", "", 0.2, 0)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Status != SubagentStatusSuccess {
			t.Fatalf("task %d failed: %s", i, r.Error)
		}
	}
}

func TestDelegateParallelToolDefinition(t *testing.T) {
	t.Parallel()

	def := DelegateParallelToolDefinition()
	if def.Name != "delegate_parallel" {
		t.Fatalf("expected name 'delegate_parallel', got %q", def.Name)
	}
	if def.Parameters == nil {
		t.Fatal("expected parameters, got nil")
	}
	if len(def.Parameters.Required) != 1 || def.Parameters.Required[0] != "tasks" {
		t.Fatalf("expected required tasks param, got %v", def.Parameters.Required)
	}
}
