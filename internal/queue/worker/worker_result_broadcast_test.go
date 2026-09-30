package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// broadcastSink records both persisted and broadcast events, so a test can
// assert that RESULT is broadcast but NOT persisted a second time.
type broadcastSink struct {
	emitted     []models.Event
	broadcasted []models.Event
}

func (s *broadcastSink) Emit(_ context.Context, ev models.Event) error {
	s.emitted = append(s.emitted, ev)
	return nil
}

func (s *broadcastSink) Broadcast(_ context.Context, ev models.Event) error {
	s.broadcasted = append(s.broadcasted, ev)
	return nil
}

// persistOnlySink implements EventSink but deliberately not EventBroadcaster,
// standing in for a deployment whose sink cannot fan out to live subscribers.
type persistOnlySink struct{ emitted []models.Event }

func (s *persistOnlySink) Emit(_ context.Context, ev models.Event) error {
	s.emitted = append(s.emitted, ev)
	return nil
}

func resultEventCount(sink *broadcastSink) int {
	n := 0
	for _, ev := range sink.broadcasted {
		if ev.Type == models.EventTypeResult {
			n++
		}
	}
	return n
}

// TestCommitSucceededBroadcastsResult covers the fix for the J14 finding: a
// completed task's RESULT event was persisted by the store transaction but
// never published to the event bus, so SSE subscribers never learned the task
// finished. It must now be broadcast — exactly once — without being persisted
// again (the store already wrote the row).
func TestCommitSucceededBroadcastsResult(t *testing.T) {
	store := testutil.NewFakeStore()
	sink := &broadcastSink{}
	w := &Worker{store: store, sink: sink}
	task := seedCommitTask(t, store)

	ok := w.commitSucceeded(context.Background(), task, sandbox.Result{
		Success:  true,
		ExitCode: 0,
		Stdout:   "hello",
	}, nil)
	if !ok {
		t.Fatal("commitSucceeded() = false, want true")
	}

	if got := resultEventCount(sink); got != 1 {
		t.Fatalf("broadcast RESULT events = %d, want 1 (live subscribers must be told the task completed)", got)
	}
	for _, ev := range sink.broadcasted {
		if ev.Type != models.EventTypeResult {
			continue
		}
		if !ev.TaskID.Valid || ev.TaskID.String != task.ID {
			t.Fatalf("broadcast RESULT task_id = %#v, want %q", ev.TaskID, task.ID)
		}
		if ev.ProjectID != task.ProjectID {
			t.Fatalf("broadcast RESULT project_id = %q, want %q", ev.ProjectID, task.ProjectID)
		}
	}
	// The store wrote the row inside UpdateTaskResult; re-emitting would
	// duplicate it in the durable event log.
	for _, ev := range sink.emitted {
		if ev.Type == models.EventTypeResult {
			t.Fatal("RESULT was persisted a second time via Emit; it is already written by the store transaction")
		}
	}
}

// TestCommitSucceededSkipsBroadcastWithoutBroadcaster guards the type
// assertion: a sink that only persists must not panic or error, and the
// commit must still be reported as successful.
func TestCommitSucceededSkipsBroadcastWithoutBroadcaster(t *testing.T) {
	store := testutil.NewFakeStore()
	sink := &persistOnlySink{}
	w := &Worker{store: store, sink: sink}
	task := seedCommitTask(t, store)

	if ok := w.commitSucceeded(context.Background(), task, sandbox.Result{Success: true}, nil); !ok {
		t.Fatal("commitSucceeded() = false, want true even without a broadcaster")
	}
}

// failingResultStore rejects UpdateTaskResult, standing in for the optimistic
// lock rejecting a stale write. testutil.FakeKanbanStore ignores
// expectedUpdatedAt, so the failure has to be injected here.
type failingResultStore struct {
	*testutil.FakeKanbanStore
}

func (f *failingResultStore) UpdateTaskResult(
	_ context.Context, _ string, _ time.Time, _ models.TaskResult,
) (*models.Task, error) {
	return nil, models.ErrStateConflict
}

// TestCommitSucceededNoBroadcastOnUpdateFailure ensures a failed persistence
// is not announced as a completion: the RESULT broadcast happens only after
// UpdateTaskResult succeeds.
func TestCommitSucceededNoBroadcastOnUpdateFailure(t *testing.T) {
	base := testutil.NewFakeStore()
	store := &failingResultStore{FakeKanbanStore: base}
	sink := &broadcastSink{}
	w := &Worker{store: store, sink: sink}
	task := seedCommitTask(t, base)

	if ok := w.commitSucceeded(context.Background(), task, sandbox.Result{Success: true}, nil); ok {
		t.Fatal("commitSucceeded() = true, want false when the result was not persisted")
	}
	if got := resultEventCount(sink); got != 0 {
		t.Fatalf("broadcast RESULT events = %d, want 0 (nothing was persisted, so nothing may be announced)", got)
	}
}

// TestEvictBroadcastsResult covers the failure side of the same gap: evict
// writes a RESULT event through the store transaction, so live subscribers
// need it too — otherwise the board shows a task as running right up to the
// moment it becomes FAILED_REQUIRES_HUMAN.
func TestEvictBroadcastsResult(t *testing.T) {
	store := testutil.NewFakeStore()
	sink := &broadcastSink{}
	w := &Worker{store: store, sink: sink, maxRetries: 1}
	task := seedCommitTask(t, store)

	w.evict(context.Background(), task, "boom")

	if got := resultEventCount(sink); got != 1 {
		t.Fatalf("broadcast RESULT events = %d, want 1 (eviction is a terminal result subscribers must see)", got)
	}
}

// TestFailHardBroadcastsResult covers the FailHard path, which records a
// terminal result without any further state transition.
func TestFailHardBroadcastsResult(t *testing.T) {
	store := testutil.NewFakeStore()
	sink := &broadcastSink{}
	w := &Worker{store: store, sink: sink}
	task := seedCommitTask(t, store)

	w.FailHard(context.Background(), task, errors.New("gateway down"))

	if got := resultEventCount(sink); got != 1 {
		t.Fatalf("broadcast RESULT events = %d, want 1", got)
	}
}

// seedCommitTask creates a RUNNING task the worker can commit against.
func seedCommitTask(t *testing.T, store *testutil.FakeKanbanStore) models.Task {
	t.Helper()

	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "result-broadcast",
		Tasks:       []models.DraftTask{{Title: "commit me", Description: "task to commit"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	return *running
}
