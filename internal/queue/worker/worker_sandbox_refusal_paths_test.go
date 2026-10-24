package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"
)

func refusedWithoutJail() error {
	return fmt.Errorf("%w: sandbox.backend=bubblewrap is required, but bwrap not found; command refused", models.ErrSandboxUnavailable)
}

// assertRefusedTerminal checks the B-051 contract for a recovery path that hit a
// refusing jail: the task is FAILED with the reason, not parked for a human,
// requeued or handed a permission/prompt handoff.
func assertRefusedTerminal(t *testing.T, w *Worker, sink *mockEventSink, taskID string) {
	t.Helper()
	ctx := context.Background()
	got, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailed {
		t.Fatalf("task state = %s, want %s: a refused jail must end the task terminally", got.State, models.TaskStateFailed)
	}
	for _, typ := range []string{"RETRY", string(models.EventTypePermissionDetected)} {
		if n := eventCount(sink, typ); n != 0 {
			t.Fatalf("%s events = %d, want 0", typ, n)
		}
	}
	comments, err := w.store.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "not be retried") {
		t.Fatalf("comments = %#v, want one terminal failure comment", comments)
	}
}

func eventCount(sink *mockEventSink, typ string) int {
	n := 0
	for _, ev := range sink.events {
		if string(ev.Type) == typ {
			n++
		}
	}
	return n
}
