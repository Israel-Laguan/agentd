package agentic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
	args := map[string]string{"path": path}
	argsJSON, _ := json.Marshal(args)
	return gateway.ToolCall{ID: id, Type: "function", Function: gateway.ToolCallFunction{Name: "read", Arguments: string(argsJSON)}}
}

func writeCall(id, path, content string) gateway.ToolCall {
	args := map[string]string{"path": path, "content": content}
	argsJSON, _ := json.Marshal(args)
	return gateway.ToolCall{ID: id, Type: "function", Function: gateway.ToolCallFunction{Name: "write", Arguments: string(argsJSON)}}
}

// cutReadWorkspace is a workspace holding tabwriter.go with content, and a
// dispatch that runs every call on it, recording the call IDs it ran.
func cutReadWorkspace(t *testing.T, content string) (string, *[]string, func(gateway.ToolCall) (agenttools.ToolResult, bool)) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tabwriter.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := agenttools.NewToolExecutor(nil, dir, nil, 0)
	var dispatched []string
	return dir, &dispatched, func(c gateway.ToolCall) (agenttools.ToolResult, bool) {
		dispatched = append(dispatched, c.ID)
		return agenttools.ClassifyBuiltinToolResult(c.ID, c.Function.Name, ex.Execute(context.Background(), c), 0), false
	}
}

// readTurn is the conversation after the model read a file: the read call and
// the content it returned.
func readTurn(id, path, content string) []gateway.PromptMessage {
	return []gateway.PromptMessage{
		{Role: "assistant", Content: "c", ToolCalls: []gateway.ToolCall{readCall(id, path)}},
		{Role: "tool", ToolCallID: id, Content: content},
	}
}

func generateWriteBack(t *testing.T, e *Engine, msgs []gateway.PromptMessage) gateway.ToolCall {
	t.Helper()
	msgs = append([]gateway.PromptMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "task"}}, msgs...)
	resp, stop, err := e.generateAgenticTurn(context.Background(), models.Task{}, models.AgentProfile{}, &msgs, nil,
		agentruntime.NewBudgetGuard(nil, ""), agentruntime.NewContextBudgetGuard(1000, 0.8), 0, 0)
	if err != nil || stop != nil {
		t.Fatalf("generateAgenticTurn: stop=%v err=%v", stop, err)
	}
	return resp.ToolCalls[0]
}

// B-089: the budget cut the read the model saw, so a whole-file write would
// replace the file with that cut copy and delete the rest. The write is refused
// and never reaches the executor.
func TestWriteToAFileWhoseReadWasCutIsRefused(t *testing.T) {
	gw := &writeBackGateway{call: writeCall("w1", "tabwriter.go", "short")}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	long := strings.Repeat("x", 5000)
	_, dispatched, dispatch := cutReadWorkspace(t, long)
	ctx := context.Background()
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("1", "tabwriter.go"), dispatch)
	write := generateWriteBack(t, e, readTurn("1", "tabwriter.go", long))
	if toolContent(gw.seen, "1") == long {
		t.Fatal("the model's request carries the whole read; the test would prove nothing")
	}
	tr, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, write, dispatch)
	if len(*dispatched) != 1 || tr.Status == agenttools.ToolStatusSuccess {
		t.Fatalf("dispatched = %v, status = %v; want the write refused before it runs", *dispatched, tr.Status)
	}
}

// Once the model has read the file again and was shown all of it, a write to it
// is allowed: the latest read decides.
func TestWriteAfterAnUncutReadIsAllowed(t *testing.T) {
	gw := &writeBackGateway{call: writeCall("w2", "tabwriter.go", "short")}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	long := strings.Repeat("x", 5000)
	dir, dispatched, dispatch := cutReadWorkspace(t, long)
	ctx := context.Background()
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("1", "tabwriter.go"), dispatch)
	e.noteCutReads([]gateway.PromptMessage{{Role: "tool", ToolCallID: "1", Content: "cut"}})
	if !e.cutReads["tabwriter.go"] {
		t.Fatal("the first read was not marked cut; the test would not exercise clearing")
	}
	// The model shortened the file with a shell command, then read all of it.
	if err := os.WriteFile(filepath.Join(dir, "tabwriter.go"), []byte("package tabwriter"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("2", "./tabwriter.go"), dispatch)
	write := generateWriteBack(t, e, append(readTurn("1", "tabwriter.go", long), readTurn("2", "./tabwriter.go", "package tabwriter")...))
	tr, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, write, dispatch)
	if last := (*dispatched)[len(*dispatched)-1]; last != "w2" || tr.Status != agenttools.ToolStatusSuccess {
		t.Fatalf("dispatched = %v, status = %v; want the write to run after a whole read", *dispatched, tr.Status)
	}
}

// A file whose own text holds the cut marker was shown whole: the write is
// allowed. Only the cut decides, not the text it leaves.
func TestWriteToAFileHoldingTheCutMarkerIsAllowed(t *testing.T) {
	gw := &writeBackGateway{call: writeCall("w3", "marker.go", "short")}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	content := "const m = \"" + truncation.TruncationMarker + "\""
	dir, dispatched, dispatch := cutReadWorkspace(t, "")
	if err := os.WriteFile(filepath.Join(dir, "marker.go"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("1", "marker.go"), dispatch)
	write := generateWriteBack(t, e, readTurn("1", "marker.go", content))
	if toolContent(gw.seen, "1") != content {
		t.Fatal("the model's request cut the read; the test would prove nothing")
	}
	tr, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, write, dispatch)
	if last := (*dispatched)[len(*dispatched)-1]; last != "w3" || tr.Status != agenttools.ToolStatusSuccess {
		t.Fatalf("dispatched = %v, status = %v; want the write to run", *dispatched, tr.Status)
	}
}

// A reread that fails shows the model none of the file, so the cut mark stays
// and the write is still refused.
func TestWriteAfterAFailedRereadIsRefused(t *testing.T) {
	gw := &writeBackGateway{call: writeCall("w4", "tabwriter.go", "short")}
	e := &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}}
	long := strings.Repeat("x", 5000)
	dir, dispatched, dispatch := cutReadWorkspace(t, long)
	ctx := context.Background()
	e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("1", "tabwriter.go"), dispatch)
	e.noteCutReads([]gateway.PromptMessage{{Role: "tool", ToolCallID: "1", Content: "cut"}})
	if err := os.Chmod(filepath.Join(dir, "tabwriter.go"), 0); err != nil {
		t.Fatal(err)
	}
	reread, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, readCall("2", "tabwriter.go"), dispatch)
	if reread.Status == agenttools.ToolStatusSuccess {
		t.Skip("the reread did not fail (running as root?); the test would prove nothing")
	}
	write := generateWriteBack(t, e, readTurn("2", "tabwriter.go", reread.ForContext()))
	tr, _, _ := e.runAgenticCall(ctx, models.Task{}, models.AgentProfile{}, write, dispatch)
	if last := (*dispatched)[len(*dispatched)-1]; last == "w4" || tr.Status == agenttools.ToolStatusSuccess {
		t.Fatalf("dispatched = %v, status = %v; want the write refused after a failed reread", *dispatched, tr.Status)
	}
}
