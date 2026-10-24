package worker

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// dedupeCountingSandbox records how many commands reach the sandbox so tests prove
// short-circuited attempts never execute.
type dedupeCountingSandbox struct {
	result sandbox.Result
	err    error
	calls  int
}

func (s *dedupeCountingSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	s.calls++
	return s.result, s.err
}

func seedCommandHistory(t *testing.T, store *testutil.FakeKanbanStore, task models.Task, command, failure string) {
	t.Helper()
	if err := store.AppendEvent(context.Background(), models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventTypeCommand,
		Payload:   command,
	}); err != nil {
		t.Fatalf("seed COMMAND event: %v", err)
	}
	if err := store.AppendEvent(context.Background(), models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      "RETRY",
		Payload:   failure,
	}); err != nil {
		t.Fatalf("seed RETRY event: %v", err)
	}
}

func runDedupeCommand(t *testing.T, store *testutil.FakeKanbanStore, sb *dedupeCountingSandbox, task models.Task, command string) *mockEventSink {
	t.Helper()
	ctx := context.Background()
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	sink := &mockEventSink{}
	w := NewWorker(store, &routingTestGateway{}, sb, nil, sink, WorkerOptions{})
	w.executeLegacyCommand(ctx, task, *project, models.AgentProfile{}, command, &legacyTaskAudit{})
	return sink
}

func sinkHasType(sink *mockEventSink, want models.EventType) bool {
	for _, ev := range sink.events {
		if ev.Type == want {
			return true
		}
	}
	return false
}

// B-043: sandbox refusals repeat identically too (S5/S8 refused heredoc x3);
// the refused command is recorded in SANDBOX_VIOLATION, not COMMAND.
func TestExecuteLegacyCommand_IdenticalRefusalEvictsEarly(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "dedupe-refuse", "refused heredoc")
	retried, err := store.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
	if err != nil {
		t.Fatalf("IncrementRetryCount: %v", err)
	}
	const command = "cat <<'EOF' > out.txt\n// data\nEOF"
	if err := store.AppendEvent(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      "SANDBOX_VIOLATION",
		Payload:   "absolute path escapes workspace: //; command: " + command,
	}); err != nil {
		t.Fatalf("seed SANDBOX_VIOLATION event: %v", err)
	}

	sb := &dedupeCountingSandbox{result: sandbox.Result{ExitCode: -1}, err: fmt.Errorf("%w: absolute path escapes workspace: //", models.ErrSandboxViolation)}
	sink := runDedupeCommand(t, store, sb, *retried, command)

	if sb.calls != 0 {
		t.Fatalf("sandbox executions = %d, want 0 (identical refusal must not re-execute)", sb.calls)
	}
	if !sinkHasType(sink, "POISON_PILL_HANDOFF") {
		t.Fatalf("sink events = %#v, want POISON_PILL_HANDOFF terminal handoff", sink.events)
	}
}

// B-043: an empty model command must never reach the sandbox (S5's empty
// fourth attempt executed as `RESULT command is required`).
func TestExecuteLegacyCommand_EmptyCommandNeverExecutes(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "dedupe-empty", "empty command")
	sb := &dedupeCountingSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}
	sink := runDedupeCommand(t, store, sb, task, "   ")

	if sb.calls != 0 {
		t.Fatalf("sandbox executions = %d, want 0 (empty command must not execute)", sb.calls)
	}
	if sinkHasType(sink, models.EventTypeCommand) {
		t.Fatalf("sink events = %#v, want no COMMAND event for an empty command", sink.events)
	}
	after, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.State == models.TaskStateCompleted {
		t.Fatal("task COMPLETED on an empty command, want failure handling")
	}
}

func TestExecuteLegacyCommand_DedupeMessageNamesRepeat(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "dedupe-msg", "message check")
	retried, err := store.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
	if err != nil {
		t.Fatalf("IncrementRetryCount: %v", err)
	}
	seedCommandHistory(t, store, *retried, "go build ./...", "exit=1")
	sb := &dedupeCountingSandbox{result: sandbox.Result{ExitCode: 1}}
	sink := runDedupeCommand(t, store, sb, *retried, "go build ./...")

	found := false
	for _, ev := range sink.events {
		if ev.Type == "POISON_PILL_HANDOFF" && strings.Contains(ev.Payload, "go build") {
			found = true
		}
	}
	if !found {
		t.Fatalf("sink events = %#v, want handoff naming the repeated command", sink.events)
	}
}
