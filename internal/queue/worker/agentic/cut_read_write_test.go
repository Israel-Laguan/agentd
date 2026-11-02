package agentic

import (
	"context"
	"strings"
	"testing"

	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/gateway/truncation"
	"agentd/internal/models"
)

// writeBackGateway answers every turn with the same whole-file write: the model
// writing back the copy of the file it was shown.
type writeBackGateway struct {
	gateway.AIGateway
	call gateway.ToolCall
	seen []gateway.PromptMessage
}

func (g *writeBackGateway) Generate(_ context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.seen = req.Messages
	return gateway.AIResponse{ToolCalls: []gateway.ToolCall{g.call}}, nil
}

func toolContent(msgs []gateway.PromptMessage, callID string) string {
	for _, m := range msgs {
		if m.Role == "tool" && m.ToolCallID == callID {
			return m.Content
		}
	}
	return ""
}

func readCall(id, path string) gateway.ToolCall {
	return gateway.ToolCall{ID: id, Type: "function", Function: gateway.ToolCallFunction{Name: "read", Arguments: `{"path":"` + path + `"}`}}
}

func writeCall(id, path, content string) gateway.ToolCall {
	return gateway.ToolCall{ID: id, Type: "function", Function: gateway.ToolCallFunction{Name: "write", Arguments: `{"path":"` + path + `","content":"` + content + `"}`}}
}

// B-089: the budget cut the read the model saw, so a whole-file write would
// replace the file with that cut copy and delete the rest. The write is refused
// and never reaches the executor.
func TestWriteToAFileWhoseReadWasCutIsRefused(t *testing.T) {
	gw := &writeBackGateway{call: writeCall("w1", "tabwriter.go", "short")}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	msgs := []gateway.PromptMessage{
		{Role: "system", Content: "s"}, {Role: "user", Content: "task"},
		{Role: "assistant", Content: "c", ToolCalls: []gateway.ToolCall{readCall("1", "tabwriter.go")}},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("x", 5000)},
	}
	var dispatched []string
	dispatch := func(c gateway.ToolCall) (agenttools.ToolResult, bool) {
		dispatched = append(dispatched, c.ID)
		return agenttools.ToolResult{Status: agenttools.ToolStatusSuccess}, false
	}
	ctx := context.Background()
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("1", "tabwriter.go"), dispatch)
	resp, stop, err := e.generateAgenticTurn(ctx, models.Task{}, models.AgentProfile{}, &msgs, nil,
		agentruntime.NewBudgetGuard(nil, ""), agentruntime.NewContextBudgetGuard(1000, 0.8), 0, 0)
	if err != nil || stop != nil {
		t.Fatalf("generateAgenticTurn: stop=%v err=%v", stop, err)
	}
	if !strings.Contains(toolContent(gw.seen, "1"), truncation.TruncationMarker) {
		t.Fatal("the model's request does not carry the cut read; the test would prove nothing")
	}
	tr, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, resp.ToolCalls[0], dispatch)
	if len(dispatched) != 1 || tr.Status == agenttools.ToolStatusSuccess {
		t.Fatalf("dispatched = %v, status = %v; want the write refused before it runs", dispatched, tr.Status)
	}
}

// Once the model has read the file again and was shown all of it, a write to it
// is allowed: the latest read decides.
func TestWriteAfterAnUncutReadIsAllowed(t *testing.T) {
	gw := &writeBackGateway{call: writeCall("w2", "tabwriter.go", "short")}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	msgs := []gateway.PromptMessage{
		{Role: "system", Content: "s"}, {Role: "user", Content: "task"},
		{Role: "assistant", Content: "c", ToolCalls: []gateway.ToolCall{readCall("1", "tabwriter.go")}},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("x", 5000)},
		{Role: "assistant", Content: "again", ToolCalls: []gateway.ToolCall{readCall("2", "./tabwriter.go")}},
		{Role: "tool", ToolCallID: "2", Content: "package tabwriter"},
	}
	var dispatched []string
	dispatch := func(c gateway.ToolCall) (agenttools.ToolResult, bool) {
		dispatched = append(dispatched, c.ID)
		return agenttools.ToolResult{Status: agenttools.ToolStatusSuccess}, false
	}
	ctx := context.Background()
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("1", "tabwriter.go"), dispatch)
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("2", "./tabwriter.go"), dispatch)
	resp, stop, err := e.generateAgenticTurn(ctx, models.Task{}, models.AgentProfile{}, &msgs, nil,
		agentruntime.NewBudgetGuard(nil, ""), agentruntime.NewContextBudgetGuard(1000, 0.8), 0, 0)
	if err != nil || stop != nil {
		t.Fatalf("generateAgenticTurn: stop=%v err=%v", stop, err)
	}
	tr, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, resp.ToolCalls[0], dispatch)
	if dispatched[len(dispatched)-1] != "w2" || tr.Status != agenttools.ToolStatusSuccess {
		t.Fatalf("dispatched = %v, status = %v; want the write to run after a whole read", dispatched, tr.Status)
	}
}
