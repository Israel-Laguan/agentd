package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// alwaysRefusingSandbox refuses every command like an unavailable required
// jail. It is safe for the concurrent children of delegate_parallel.
type alwaysRefusingSandbox struct {
	mu    sync.Mutex
	calls int
}

func (s *alwaysRefusingSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return sandbox.Result{ExitCode: -1}, refusedWithoutJail()
}

// delegatingGateway scripts one parent that delegates once and children that
// run one bash call, then answer. Calls are counted per role.
type delegatingGateway struct {
	subagentTaskGateway
	delegateArgs string
	toolName     string
	mu           sync.Mutex
	parentCalls  int
	childCalls   int
}

func (g *delegatingGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.HasPrefix(req.Messages[0].Content, "You are a specialized subagent") {
		g.childCalls++
		if req.Messages[len(req.Messages)-1].Role == "user" {
			return bashCall("child-bash", "uname -a"), nil
		}
		return gateway.AIResponse{Content: "child done"}, nil
	}
	g.parentCalls++
	if g.parentCalls == 1 {
		return gateway.AIResponse{Content: "delegating", ToolCalls: []gateway.ToolCall{{
			ID: "d1", Type: "function",
			Function: gateway.ToolCallFunction{Name: g.toolName, Arguments: g.delegateArgs},
		}}}, nil
	}
	return gateway.AIResponse{Content: "parent done"}, nil
}

func (g *delegatingGateway) ProviderSupportsChatTools(provider string) bool {
	return strings.TrimSpace(provider) != ""
}

func seedSubagent(workspace string) {
	dir := filepath.Join(workspace, ".agentd", "subagents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		panic(err)
	}
	def := "# Subagent: inspector\n\n## Purpose\n\ninspect the host\n"
	if err := os.WriteFile(filepath.Join(dir, "inspector.md"), []byte(def), 0o644); err != nil {
		panic(err)
	}
}

// B-052 follow-up: a child's bash call refused by an unavailable required jail
// must end the PARENT task terminally. The child result is not an ordinary
// delegate error the parent can retry or complete past.
func TestDelegatedBashFailsParentTerminalWhenSandboxUnavailable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, tool, args string
	}{
		{"delegate", "delegate", `{"subagent":"inspector","task":"uname"}`},
		{"delegate_parallel", "delegate_parallel", `{"tasks":[{"subagent":"inspector","task":"a"},{"subagent":"inspector","task":"b"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gw := &delegatingGateway{toolName: tt.tool, delegateArgs: tt.args}
			sb := &alwaysRefusingSandbox{}
			w, sink, task := runAgenticTaskIn(t, gw, sb, seedSubagent)
			assertAgenticRefusedTerminal(t, w, task.ID)
			assertRefusedTerminal(t, w, sink, task.ID)
			if gw.parentCalls != 1 {
				t.Fatalf("parent model calls = %d, want 1: the parent must not be called again after the refusal", gw.parentCalls)
			}
			if gw.childCalls < 1 || sb.calls < 1 {
				t.Fatalf("child calls = %d sandbox calls = %d, want the child to reach the jail", gw.childCalls, sb.calls)
			}
		})
	}
}
