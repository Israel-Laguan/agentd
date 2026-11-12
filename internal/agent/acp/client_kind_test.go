package acp

import (
	"context"
	"testing"

	acpsdk "github.com/coder/acp-go-sdk"
)

// An explicit kind in a permission request replaces the recorded one, so a later
// request that omits it, and the evidence count, see the latest kind.
func TestExplicitPermissionKindReplacesTheRecordedOne(t *testing.T) {
	t.Parallel()
	c := &client{sessions: map[acpsdk.SessionId]*session{}}
	s := c.open(context.Background(), "s", Request{NoWorkspace: true})
	id := acpsdk.ToolCallId("c1")
	read, edit, done := acpsdk.ToolKindRead, acpsdk.ToolKindEdit, acpsdk.ToolCallStatusCompleted
	s.noteToolCall(id, &read, nil)
	if _, err := c.RequestPermission(context.Background(), acpsdk.RequestPermissionRequest{SessionId: "s", ToolCall: acpsdk.ToolCallUpdate{ToolCallId: id, Kind: &edit}}); err != nil {
		t.Fatal(err)
	}
	if got := s.recordedKind(id); got != edit {
		t.Fatalf("recorded kind = %q; want %q", got, edit)
	}
	s.noteToolCall(id, nil, &done)
	if n := s.evidence(); n != 1 {
		t.Fatalf("evidence = %d; want the completed edit counted", n)
	}
}
