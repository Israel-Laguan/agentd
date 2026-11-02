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

// cutReadWorkspace is a workspace holding tabwriter.go, and an engine whose
// calls run on it. dispatched records the IDs of the calls that ran.
type cutReadWorkspace struct {
	e          *Engine
	gw         *writeBackGateway
	dir        string
	ex         *agenttools.ToolExecutor
	dispatched []string
}

// newCutReadWorkspace answers every model turn with a whole-file write to path.
func newCutReadWorkspace(t *testing.T, content, path string) *cutReadWorkspace {
	t.Helper()
	gw := &writeBackGateway{call: writeCall("w", path, "short")}
	w := &cutReadWorkspace{
		e:   &Engine{config: Config{Gateway: gw, TruncatorMax: 30, CharacterBudget: 300}, host: &noopHost{}},
		gw:  gw,
		dir: t.TempDir(),
	}
	w.ex = agenttools.NewToolExecutor(nil, w.dir, nil, 0)
	w.writeFile(t, "tabwriter.go", content)
	return w
}

func (w *cutReadWorkspace) writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(w.dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (w *cutReadWorkspace) run(c gateway.ToolCall) agenttools.ToolResult {
	dispatch := func(c gateway.ToolCall) (agenttools.ToolResult, bool) {
		w.dispatched = append(w.dispatched, c.ID)
		return agenttools.ClassifyBuiltinToolResult(c.ID, c.Function.Name, w.ex.Execute(context.Background(), c), 0), false
	}
	tr, _, _ := w.e.runAgenticCall(context.Background(), models.Task{}, models.AgentProfile{}, c, w.ex, dispatch)
	return tr
}

// read runs a read call and returns the conversation after it: the call and the
// content it returned.
func (w *cutReadWorkspace) read(id, path string) []gateway.PromptMessage {
	tr := w.run(readCall(id, path))
	return []gateway.PromptMessage{
		{Role: "assistant", Content: "c", ToolCalls: []gateway.ToolCall{readCall(id, path)}},
		{Role: "tool", ToolCallID: id, Content: tr.ForContext()},
	}
}

// generate runs a model turn over msgs and returns the write it answers with.
func (w *cutReadWorkspace) generate(t *testing.T, msgs ...[]gateway.PromptMessage) gateway.ToolCall {
	t.Helper()
	all := []gateway.PromptMessage{{Role: "system", Content: "s"}, {Role: "user", Content: "task"}}
	for _, m := range msgs {
		all = append(all, m...)
	}
	resp, stop, err := w.e.generateAgenticTurn(context.Background(), models.Task{}, models.AgentProfile{}, &all, nil,
		agentruntime.NewBudgetGuard(nil, ""), agentruntime.NewContextBudgetGuard(1000, 0.8), 0, 0)
	if err != nil || stop != nil {
		t.Fatalf("generateAgenticTurn: stop=%v err=%v", stop, err)
	}
	return resp.ToolCalls[0]
}

// wrote reports whether the last call that ran was the model's write, and it
// passed.
func (w *cutReadWorkspace) wrote(tr agenttools.ToolResult) bool {
	return w.dispatched[len(w.dispatched)-1] == "w" && tr.Status == agenttools.ToolStatusSuccess
}

// B-089: the budget cut the read the model saw, so a whole-file write would
// replace the file with that cut copy and delete the rest. The write is refused
// and never reaches the executor.
func TestWriteToAFileWhoseReadWasCutIsRefused(t *testing.T) {
	long := strings.Repeat("x", 5000)
	w := newCutReadWorkspace(t, long, "tabwriter.go")
	write := w.generate(t, w.read("1", "tabwriter.go"))
	if toolContent(w.gw.seen, "1") == long {
		t.Fatal("the model's request carries the whole read; the test would prove nothing")
	}
	if tr := w.run(write); w.wrote(tr) {
		t.Fatalf("dispatched = %v; want the write refused before it runs", w.dispatched)
	}
}

// Once the model has read the file again and was shown all of it, a write to it
// is allowed: the latest read decides.
func TestWriteAfterAnUncutReadIsAllowed(t *testing.T) {
	long := strings.Repeat("x", 5000)
	w := newCutReadWorkspace(t, long, "tabwriter.go")
	first := w.read("1", "tabwriter.go")
	if tr := w.run(w.generate(t, first)); w.wrote(tr) {
		t.Fatal("the first read was not taken as cut; the test would not exercise clearing")
	}
	// The model shortened the file with a shell command, then read all of it.
	w.writeFile(t, "tabwriter.go", "package tabwriter")
	write := w.generate(t, first, w.read("2", "./tabwriter.go"))
	if tr := w.run(write); !w.wrote(tr) {
		t.Fatalf("dispatched = %v, status = %v; want the write to run after a whole read", w.dispatched, tr.Status)
	}
}

// A file whose own text holds the cut marker was shown whole: the write is
// allowed. Only the cut decides, not the text it leaves.
func TestWriteToAFileHoldingTheCutMarkerIsAllowed(t *testing.T) {
	content := "const m = \"" + truncation.TruncationMarker + "\""
	w := newCutReadWorkspace(t, content, "tabwriter.go")
	write := w.generate(t, w.read("1", "tabwriter.go"))
	if toolContent(w.gw.seen, "1") != content {
		t.Fatal("the model's request cut the read; the test would prove nothing")
	}
	if tr := w.run(write); !w.wrote(tr) {
		t.Fatalf("dispatched = %v, status = %v; want the write to run", w.dispatched, tr.Status)
	}
}

// A reread that fails shows the model none of the file, so the cut mark stays
// and the write is still refused.
func TestWriteAfterAFailedRereadIsRefused(t *testing.T) {
	w := newCutReadWorkspace(t, strings.Repeat("x", 5000), "tabwriter.go")
	w.generate(t, w.read("1", "tabwriter.go"))
	// The file stays where it is, so the write still names it; only the reread
	// fails, as it does for any user, root included.
	w.ex.SetMaxReadBytes(100)
	reread := w.read("2", "tabwriter.go")
	if !strings.Contains(reread[1].Content, "too large") {
		t.Fatalf("reread = %q, want it to fail; the test would prove nothing", reread[1].Content)
	}
	if tr := w.run(w.generate(t, reread)); w.wrote(tr) {
		t.Fatalf("dispatched = %v; want the write refused after a failed reread", w.dispatched)
	}
}

// A file has many names: a symlink to it, or the path the symlink names. A read
// cut under one name refuses a write under another, since both reach one file.
func TestWriteThroughAnotherNameOfACutFileIsRefused(t *testing.T) {
	for _, tc := range []struct{ read, write string }{
		{read: "link.go", write: "tabwriter.go"},
		{read: "tabwriter.go", write: "link.go"},
		{read: "dir/tabwriter.go", write: "./tabwriter.go"},
	} {
		t.Run(tc.read+" then "+tc.write, func(t *testing.T) {
			w := newCutReadWorkspace(t, strings.Repeat("x", 5000), tc.write)
			if err := os.Symlink("tabwriter.go", filepath.Join(w.dir, "link.go")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(".", filepath.Join(w.dir, "dir")); err != nil {
				t.Fatal(err)
			}
			if tr := w.run(w.generate(t, w.read("1", tc.read))); w.wrote(tr) {
				t.Fatalf("dispatched = %v; want the write refused", w.dispatched)
			}
		})
	}
}
