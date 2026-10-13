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

// B-043: a retry that would send the exact command that just failed must stop
// early instead of burning the full budget on a deterministic failure.
func TestExecuteLegacyCommand_IdenticalRetryEvictsEarly(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "dedupe-proj", "deterministic build")
	retried, err := store.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
	if err != nil {
		t.Fatalf("IncrementRetryCount: %v", err)
	}
	const command = "go build ./..."
	const failure = "exit=1\nmain.go:12: undefined: CountWords"
	seedCommandHistory(t, store, *retried, command, failure)

	sb := &dedupeCountingSandbox{result: sandbox.Result{ExitCode: 1, Stdout: "", Stderr: "main.go:12: undefined: CountWords"}}
	sink := runDedupeCommand(t, store, sb, *retried, command)

	if sb.calls != 0 {
		t.Fatalf("sandbox executions = %d, want 0 (identical retry must not re-execute)", sb.calls)
	}
	if !sinkHasType(sink, "POISON_PILL_HANDOFF") {
		t.Fatalf("sink events = %#v, want POISON_PILL_HANDOFF terminal handoff", sink.events)
	}
	after, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("task state = %s, want FAILED_REQUIRES_HUMAN (stop early)", after.State)
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil || len(comments) == 0 {
		t.Fatalf("comments = %#v, want terminal failure comment", comments)
	}
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

// Control: a first attempt and a genuinely new command still execute.
func TestExecuteLegacyCommand_FreshCommandExecutes(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "dedupe-fresh", "fresh command")
	retried, err := store.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
	if err != nil {
		t.Fatalf("IncrementRetryCount: %v", err)
	}
	seedCommandHistory(t, store, *retried, "go build ./...", "exit=1\nold failure")

	sb := &dedupeCountingSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}
	sink := runDedupeCommand(t, store, sb, *retried, "go vet ./...")

	if sb.calls != 1 {
		t.Fatalf("sandbox executions = %d, want 1 (a new command must execute)", sb.calls)
	}
	if !sinkHasType(sink, models.EventTypeCommand) {
		t.Fatalf("sink events = %#v, want a COMMAND event", sink.events)
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
