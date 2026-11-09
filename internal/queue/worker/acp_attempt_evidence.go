package worker

import (
	"encoding/json"
	"fmt"
	"sync"

	acp "github.com/coder/acp-go-sdk"
)

type acpAttemptCall struct {
	kind          acp.ToolKind
	status        acp.ToolCallStatus
	title         string
	input, output any
	content       []acp.ToolCallContent
	locations     []acp.ToolCallLocation
}

type acpAttemptEvidence struct {
	mu    sync.Mutex
	calls map[acp.ToolCallId]*acpAttemptCall
	order []acp.ToolCallId
	plan  string
}

type acpAttemptFacts struct {
	Failures, Files []string
	Plan            string
}

func (e *acpAttemptEvidence) record(u acp.SessionUpdate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if u.Plan != nil {
		b, _ := json.Marshal(u.Plan.Entries)
		e.plan = string(b)
	}
	var id acp.ToolCallId
	var kind *acp.ToolKind
	var status *acp.ToolCallStatus
	var title *string
	var input, output any
	var content []acp.ToolCallContent
	var locations []acp.ToolCallLocation
	if c := u.ToolCall; c != nil {
		id, kind, status, title = c.ToolCallId, &c.Kind, &c.Status, &c.Title
		input, output, content, locations = c.RawInput, c.RawOutput, c.Content, c.Locations
	} else if c := u.ToolCallUpdate; c != nil {
		id, kind, status, title = c.ToolCallId, c.Kind, c.Status, c.Title
		input, output, content, locations = c.RawInput, c.RawOutput, c.Content, c.Locations
	} else {
		return
	}
	if e.calls == nil {
		e.calls = make(map[acp.ToolCallId]*acpAttemptCall)
	}
	c := e.calls[id]
	if c == nil {
		c = &acpAttemptCall{}
		e.calls[id] = c
		e.order = append(e.order, id)
	}
	if kind != nil && *kind != "" {
		c.kind = *kind
	}
	if status != nil && *status != "" {
		c.status = *status
	}
	if title != nil {
		c.title = *title
	}
	if input != nil {
		c.input = input
	}
	if output != nil {
		c.output = output
	}
	if content != nil {
		c.content = content
	}
	if locations != nil {
		c.locations = locations
	}
}

func (e *acpAttemptEvidence) result() acpAttemptFacts {
	e.mu.Lock()
	defer e.mu.Unlock()
	facts := acpAttemptFacts{Plan: e.plan}
	seen := map[string]bool{}
	for _, id := range e.order {
		c := e.calls[id]
		if c.status == acp.ToolCallStatusFailed || (c.kind == acp.ToolKindExecute && c.status == acp.ToolCallStatusCompleted && nonzeroExit(c.output)) {
			input, _ := json.Marshal(c.input)
			output, _ := json.Marshal(c.output)
			content, _ := json.Marshal(c.content)
			facts.Failures = append(facts.Failures, fmt.Sprintf("%s; input: %s; output: %s; content: %s", c.title, input, output, content))
		}
		facts.Files = appendTouchedFiles(facts.Files, seen, c)
	}
	return facts
}

func appendTouchedFiles(files []string, seen map[string]bool, c *acpAttemptCall) []string {
	// An edit still in progress when the attempt stopped may have written.
	if (c.status != acp.ToolCallStatusCompleted && c.status != acp.ToolCallStatusInProgress) || (c.kind != acp.ToolKindEdit && c.kind != acp.ToolKindDelete && c.kind != acp.ToolKindMove) {
		return files
	}
	var paths []string
	for _, loc := range c.locations {
		paths = append(paths, loc.Path)
	}
	for _, content := range c.content {
		if content.Diff != nil {
			paths = append(paths, content.Diff.Path)
		}
	}
	for _, path := range paths {
		if path != "" && !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	return files
}

// ACP does not require an exit code; only treat an explicitly reported,
// nonzero code as a failed completed command.
func nonzeroExit(output any) bool {
	fields, ok := output.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"exitCode", "exit_code"} {
		switch n := fields[key].(type) {
		case float64:
			return n != 0
		case int:
			return n != 0
		case json.Number:
			v, err := n.Int64()
			return err == nil && v != 0
		}
	}
	return false
}
