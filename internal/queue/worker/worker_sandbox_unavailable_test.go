package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// An unusable explicit jail is not a retryable agent failure: the model cannot
// fix a missing bwrap, so the task must end FAILED with the reason recorded and
// without a RETRY, SANDBOX_VIOLATION, COMMAND or SANDBOX_BACKEND event.
func TestExecuteLegacyCommandFailsTerminallyWhenSandboxUnavailable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	queued := materializeLegacyReviewTask(t, store, ctx, "sandbox-unavailable", "inspect host")
	running, err := store.MarkTaskRunning(ctx, queued.ID, queued.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	task := *running
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	reason := "sandbox.backend=bubblewrap is required, but bwrap not found; command refused"
	refused := fmt.Errorf("%w: %s", models.ErrSandboxUnavailable, reason)
	sink := &mockEventSink{}
	sb := fixedResultSandbox{result: sandbox.Result{ExitCode: -1}, err: refused}
	w := NewWorker(store, &routingTestGateway{}, sb, nil, sink, WorkerOptions{})

	w.executeLegacyCommand(ctx, task, *project, models.AgentProfile{}, "uname -a > os.txt", &legacyTaskAudit{})

	got, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailed {
		t.Fatalf("task state = %s, want %s", got.State, models.TaskStateFailed)
	}
	if got.RetryCount != 0 {
		t.Fatalf("retry count = %d, want 0: an unusable jail must not consume the retry budget", got.RetryCount)
	}
	for _, typ := range []string{"RETRY", "SANDBOX_VIOLATION", "COMMAND", "SANDBOX_BACKEND"} {
		if n := eventCount(sink, typ); n != 0 {
			t.Fatalf("%s events = %d, want 0", typ, n)
		}
	}
	events, err := store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListEventsByTask: %v", err)
	}
	if !eventPayloadContains(events, "RESULT", "bwrap not found") {
		t.Fatalf("RESULT event missing the reason: %#v", events)
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "bwrap not found") || !strings.Contains(comments[0].Body, "not be retried") {
		t.Fatalf("comments = %#v, want one terminal failure naming the reason", comments)
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

func eventPayloadContains(events []models.Event, typ, want string) bool {
	for _, ev := range events {
		if string(ev.Type) == typ && strings.Contains(ev.Payload, want) {
			return true
		}
	}
	return false
}
