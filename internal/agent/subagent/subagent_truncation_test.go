package subagent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/gateway/providers"
	"agentd/internal/gateway/routing"
	"agentd/internal/gateway/spec"
)

// A subagent trims its context with the strategy its delegate was given,
// like the parent's ContextManager, and passes it to nested delegates.
func TestEnforceSubagentContextBudget_UsesStrategy(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	long := strings.Repeat("h", 500) + strings.Repeat("t", 500)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: long},
	}

	got := enforceSubagentContextBudget(messages, len("system")+len("task")+200, strategy)

	if want := strategy.Truncate(long, 200); got[2].Content != want {
		t.Fatalf("subagent working message = %q, want head_tail cut %q", got[2].Content, want)
	}
}

func TestSubagentChild_InheritsTruncationStrategy(t *testing.T) {
	strategy := gateway.HeadTailStrategy{HeadRatio: 0.8}
	parent := NewSubagentDelegate(nil, nil, "/ws", nil, 0, 0).WithTruncationStrategy(strategy)

	child := parent.child()

	if child.truncation != strategy {
		t.Fatalf("child truncation = %#v, want %#v", child.truncation, strategy)
	}
	if child.depth != 1 {
		t.Fatalf("child depth = %d, want 1", child.depth)
	}
}

// B-089: the context budget cut the subagent's read of a file, so a whole-file
// write of it would replace the file with the cut copy. The write is refused
// and the file keeps all of its content.
func TestSubagentWriteToAFileWhoseReadWasCutIsRefused(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 5000)
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	gw := &subagentMockGateway{responses: []gateway.AIResponse{
		{ToolCalls: []gateway.ToolCall{{ID: "1", Type: "function", Function: gateway.ToolCallFunction{
			Name: "read", Arguments: `{"path":"big.go"}`}}}},
		{ToolCalls: []gateway.ToolCall{{ID: "2", Type: "function", Function: gateway.ToolCallFunction{
			Name: "write", Arguments: `{"path":"./big.go","content":"short"}`}}}},
	}}
	def := SubagentDefinition{Name: "editor", Purpose: "edit", AllowedTools: []string{"read", "write"}, ContextBudget: 500}

	if _, err := NewSubagentDelegate(gw, nil, dir, nil, 0, 0).Delegate(context.Background(), def, "edit big.go", "", "", 0.2, 0); err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	if got, _ := os.ReadFile(filepath.Join(dir, "big.go")); string(got) != long {
		t.Fatalf("big.go holds %d bytes, want the %d it had: the write saved the cut copy", len(got), len(long))
	}
}

// cuttingBackend answers each turn with the next scripted tool call, then with
// text. Its input budget is small, so the router cuts every long message.
type cuttingBackend struct {
	calls []gateway.ToolCall
	turn  int
}

func (b *cuttingBackend) Name() spec.Provider { return "cutting" }
func (b *cuttingBackend) MaxInputChars() int  { return 1000 }
func (b *cuttingBackend) Capabilities() providers.Capabilities {
	return providers.Capabilities{SupportsChatTools: true}
}

func (b *cuttingBackend) Generate(_ context.Context, _ spec.AIRequest) (spec.AIResponse, error) {
	if b.turn == len(b.calls) {
		return spec.AIResponse{Content: "done"}, nil
	}
	b.turn++
	return spec.AIResponse{ToolCalls: []spec.ToolCall{b.calls[b.turn-1]}}, nil
}

// B-089: the router cuts each message to its provider's input budget, so a
// subagent with no context_budget of its own is still shown a cut read. The
// whole-file write after it is refused.
func TestSubagentWriteAfterTheGatewayCutItsReadIsRefused(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", 5000)
	if err := os.WriteFile(filepath.Join(dir, "big.go"), []byte(long), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := &cuttingBackend{calls: []gateway.ToolCall{
		{ID: "1", Type: "function", Function: gateway.ToolCallFunction{Name: "read", Arguments: `{"path":"big.go"}`}},
		{ID: "2", Type: "function", Function: gateway.ToolCallFunction{Name: "write", Arguments: `{"path":"big.go","content":"short"}`}},
	}}
	def := SubagentDefinition{Name: "editor", Purpose: "edit", AllowedTools: []string{"read", "write"}}

	if _, err := NewSubagentDelegate(routing.NewRouter(backend), nil, dir, nil, 0, 0).Delegate(context.Background(), def, "edit big.go", "", "", 0.2, 0); err != nil {
		t.Fatalf("Delegate: %v", err)
	}

	if backend.turn != 2 {
		t.Fatalf("the backend answered %d tool calls, want 2; the test would prove nothing", backend.turn)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "big.go")); string(got) != long {
		t.Fatalf("big.go holds %d bytes, want the %d it had: the write saved the cut copy", len(got), len(long))
	}
}
