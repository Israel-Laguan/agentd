package worker

import (
	"strings"
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestACPAttemptEvidenceCombinesToolUpdates(t *testing.T) {
	var e acpAttemptEvidence
	e.record(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "c", Kind: acp.ToolKindExecute, Title: "test", RawInput: map[string]any{"command": "go test ./..."}}})
	failed := acp.ToolCallStatusFailed
	e.record(acp.SessionUpdate{ToolCallUpdate: &acp.SessionToolCallUpdate{ToolCallId: "c", Status: &failed, RawOutput: "FAIL exact output"}})
	e.record(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "edit", Kind: acp.ToolKindEdit, Status: acp.ToolCallStatusCompleted, Locations: []acp.ToolCallLocation{{Path: "handler.go"}}}})
	// An edit cut off mid-way may have written; a pending one has not run.
	e.record(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "cut", Kind: acp.ToolKindEdit, Status: acp.ToolCallStatusInProgress, Locations: []acp.ToolCallLocation{{Path: "store.go"}}}})
	e.record(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "queued", Kind: acp.ToolKindEdit, Status: acp.ToolCallStatusPending, Locations: []acp.ToolCallLocation{{Path: "never.go"}}}})
	e.record(acp.SessionUpdate{ToolCall: &acp.SessionUpdateToolCall{ToolCallId: "c2", Kind: acp.ToolKindExecute, Status: acp.ToolCallStatusCompleted, RawInput: "make test", RawOutput: map[string]any{"exitCode": float64(2)}}})
	got := e.result()
	if len(got.Failures) != 2 || !strings.Contains(got.Failures[0], "go test ./...") || !strings.Contains(got.Failures[0], "FAIL exact output") || !strings.Contains(got.Failures[1], "make test") || len(got.Files) != 2 || got.Files[0] != "handler.go" || got.Files[1] != "store.go" {
		t.Fatalf("evidence = %+v", got)
	}
}
