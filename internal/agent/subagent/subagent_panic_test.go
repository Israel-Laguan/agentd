package subagent

import (
	"context"
	"testing"

	"agentd/internal/gateway"
)

// panicGateway panics on every Generate call; it stands in for a bug in the
// gateway or a tool that a subagent goroutine reaches.
type panicGateway struct{ *subagentMockGateway }

func (panicGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	panic("boom in subagent")
}

// T-050: a panic in one parallel subagent becomes that subagent's failure
// result; it must not crash the process or leave the collector waiting.
func TestDelegateParallelRecoversPanickingSubagent(t *testing.T) {
	t.Parallel()
	delegate := NewSubagentDelegate(panicGateway{&subagentMockGateway{}}, nil, t.TempDir(), nil, 0, 0)
	tasks := []ParallelTask{
		{Definition: SubagentDefinition{Name: "a", Purpose: "first"}, Description: "task a"},
		{Definition: SubagentDefinition{Name: "b", Purpose: "second"}, Description: "task b"},
	}
	results := delegate.DelegateParallel(context.Background(), tasks, "", "", 0.2, 0)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r == nil || r.Status != SubagentStatusFailure {
			t.Fatalf("task %d result = %#v, want failure", i, r)
		}
	}
}
