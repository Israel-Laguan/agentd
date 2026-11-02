package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"agentd/internal/gateway"
)

// CutReads tracks the files whose latest read the model was shown cut to fit
// the context budget: a whole-file write to one would save the cut copy and
// delete the part the model never saw (B-089). The zero value is ready to use.
type CutReads struct {
	reads  map[string]fileRead // call ID -> what the latest read of a file read
	latest map[string]string   // real path -> the call ID of its latest read
	cut    map[string]bool     // real path -> the latest read of it was cut
}

// fileRead is what a successful read call read: the file's real path and the
// content the call returned.
type fileRead struct{ path, content string }

// NoteRead remembers a read call as it is dispatched; path is its FilePath. The
// cut can shorten the call's arguments in later requests, so the request cannot
// be the source. A failed read showed the model nothing and is not noted. Only
// the latest read of a file decides it, so the older read's content is let go.
func (c *CutReads) NoteRead(call gateway.ToolCall, path string, tr ToolResult) {
	if call.Function.Name != toolNameRead || tr.Status != ToolStatusSuccess || path == "" {
		return
	}
	if c.reads == nil {
		c.reads, c.latest, c.cut = map[string]fileRead{}, map[string]string{}, map[string]bool{}
	}
	// A provider reused the call ID of another file's read. Messages can be told
	// apart only by call ID, so that file counts as cut until it is read again.
	if r, ok := c.reads[call.ID]; ok && r.path != path {
		delete(c.latest, r.path)
		c.cut[r.path] = true
	}
	delete(c.reads, c.latest[path])
	c.reads[call.ID] = fileRead{path: path, content: tr.Content}
	c.latest[path] = call.ID
}

// Note records which files a request shows the model cut. A read is whole only
// while its message holds all the content it returned, so file text that looks
// like a cut marker is no cut. The latest read of a file decides it; an older
// read in the request is passed over. A file whose latest read is not in the
// request keeps its state, because the cut can drop that read from the window.
func (c *CutReads) Note(messages []gateway.PromptMessage) {
	latestCut := map[string]bool{} // path -> the latest read of it was cut
	for _, m := range messages {
		if r, ok := c.reads[m.ToolCallID]; ok && m.Role == "tool" {
			latestCut[r.path] = !strings.Contains(m.Content, r.content)
		}
	}
	for p, cut := range latestCut {
		if !cut {
			delete(c.cut, p)
			continue
		}
		c.cut[p] = true
	}
}

// WriteRefusal refuses a whole-file write to a file whose latest read the model
// was shown cut; path is the call's FilePath.
func (c *CutReads) WriteRefusal(call gateway.ToolCall, path string) (string, bool) {
	if call.Function.Name != toolNameWrite || !c.cut[path] {
		return "", false
	}
	return fmt.Sprintf("write refused: the read of %s was cut to fit the context budget, so a whole-file "+
		"write would delete the part you did not see. Change the file with a bash command (for example sed "+
		"or a patch), or write a new file at another path.", pathArg(call)), true
}

// FilePath returns the real path of the existing workspace file a read or write
// call names, or "". Every name of one file (./a.go, a symlink to it) returns
// the same path, the one the executor reads and writes.
func (t *ToolExecutor) FilePath(call gateway.ToolCall) string {
	if call.Function.Name != toolNameRead && call.Function.Name != toolNameWrite {
		return ""
	}
	p := pathArg(call)
	if p == "" {
		return ""
	}
	real, err := t.resolvePath(p, false)
	if err != nil {
		return ""
	}
	return real
}

func pathArg(call gateway.ToolCall) string {
	var args readArgs
	_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
	return args.Path
}
