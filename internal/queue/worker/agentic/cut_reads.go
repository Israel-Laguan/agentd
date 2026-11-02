package agentic

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/gateway/truncation"
)

// noteReadCall remembers the path of a read call as the engine dispatches it.
// The cut can shorten the call's arguments in later requests, so the request
// cannot be the source (B-089).
func (e *Engine) noteReadCall(call gateway.ToolCall) {
	if call.Function.Name != agenttools.ToolNameRead {
		return
	}
	if e.readPaths == nil {
		e.readPaths = map[string]string{}
	}
	e.readPaths[call.ID] = toolPath(call.Function.Arguments)
}

// noteCutReads records the paths the model was shown cut. The latest read of a
// path in the request decides it. A path with no read in the request keeps its
// state, because the cut can drop that read from the window (B-089).
func (e *Engine) noteCutReads(messages []gateway.PromptMessage) {
	latestCut := map[string]bool{} // path -> the latest read of it was cut
	for _, m := range messages {
		if p := e.readPaths[m.ToolCallID]; m.Role == "tool" && p != "" {
			latestCut[p] = strings.Contains(m.Content, truncation.TruncationMarker)
		}
	}
	for p, cut := range latestCut {
		if !cut {
			delete(e.cutReads, p)
			continue
		}
		if e.cutReads == nil {
			e.cutReads = map[string]bool{}
		}
		e.cutReads[p] = true
	}
}

// cutWriteRefusal refuses a whole-file write to a path whose latest read the
// model was shown cut. The write would replace the file with that copy (B-089).
func (e *Engine) cutWriteRefusal(call gateway.ToolCall) (string, bool) {
	if call.Function.Name != agenttools.ToolNameWrite {
		return "", false
	}
	p := toolPath(call.Function.Arguments)
	if !e.cutReads[p] {
		return "", false
	}
	return fmt.Sprintf("write refused: the read of %s was cut to fit the context budget, so a whole-file "+
		"write would delete the part you did not see. Change the file with a bash command (for example sed "+
		"or a patch), or write a new file at another path.", p), true
}

// toolPath returns the cleaned path argument of a read or write call, or "".
func toolPath(argsJSON string) string {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return ""
	}
	return filepath.Clean(args.Path)
}
