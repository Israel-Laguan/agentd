package worker

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// versionCheckingStore enforces the optimistic lock the real store applies to
// IncrementRetryCount and UpdateTaskState. testutil.FakeKanbanStore ignores the
// expected version on those writes, which would let the regression test below
// pass even when handleLoopResult keeps using the stale task. The mutex makes
// the check-then-write sequence atomic for callers of the wrapper.
type versionCheckingStore struct {
	*testutil.FakeKanbanStore
	mu sync.Mutex
}

func (s *versionCheckingStore) IncrementRetryCount(ctx context.Context, id string, expectedUpdatedAt time.Time) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkVersion(ctx, id, expectedUpdatedAt); err != nil {
		return nil, err
	}
	return s.FakeKanbanStore.IncrementRetryCount(ctx, id, expectedUpdatedAt)
}

func (s *versionCheckingStore) UpdateTaskState(ctx context.Context, id string, expectedUpdatedAt time.Time, next models.TaskState) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkVersion(ctx, id, expectedUpdatedAt); err != nil {
		return nil, err
	}
	return s.FakeKanbanStore.UpdateTaskState(ctx, id, expectedUpdatedAt, next)
}

func (s *versionCheckingStore) checkVersion(ctx context.Context, id string, expectedUpdatedAt time.Time) error {
	current, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if !current.UpdatedAt.Equal(expectedUpdatedAt) {
		return models.ErrStateConflict
	}
	return nil
}

func unknownLoopResult() LoopResult {
	return LoopResult{
		Status: 99,
		Meta: LoopMeta{
			LastError: "boom",
			ToolName:  "bash",
		},
	}
}

// Pre-task clarification can bump the task's version before the loop runs, so a
// loop failure must be recorded against the current row instead of leaving the
// task stuck in RUNNING on a stale optimistic lock.
func TestHandleLoopResult_RefreshesVersionBumpedByElicitation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &versionCheckingStore{FakeKanbanStore: testutil.NewFakeStore()}
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatalf("mark running: %v", err)
	}
	// The clarification answer extends the description after the caller read
	// the task, bumping the version it still holds.
	if _, err := store.UpdateTaskDescription(ctx, running.ID, running.UpdatedAt, running.Description+"\nanswer"); err != nil {
		t.Fatalf("update description: %v", err)
	}
	w := NewWorker(store, nil, nil, nil, &mockEventSink{}, WorkerOptions{MaxRetries: 2})
	w.handleLoopResult(ctx, *running, LoopResult{
		Status: LoopBudgetExhausted,
		Meta:   LoopMeta{LastError: "budget"},
	})

	updated, err := store.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.State != models.TaskStateReady {
		t.Fatalf("state = %v, want READY: the failure must use the current task version", updated.State)
	}
}

func TestHandleLoopResult_UnknownStatus_FallsThroughFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	sink := &mockEventSink{}
	w := NewWorker(store, nil, nil, nil, sink, WorkerOptions{MaxRetries: 1})
	w.handleLoopResult(ctx, task, unknownLoopResult())

	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.State != models.TaskStateFailedRequiresHuman {
		t.Errorf("state = %v, want %v", updated.State, models.TaskStateFailedRequiresHuman)
	}
	var found bool
	for _, event := range sink.events {
		if string(event.Type) == "LOOP_UNKNOWN_STATUS" {
			found = true
			if !strings.Contains(event.Payload, "status=unknown_loop_status(99)") {
				t.Errorf("payload missing status: %q", event.Payload)
			}
			if !strings.Contains(event.Payload, "last_error=boom") {
				t.Errorf("payload missing last_error: %q", event.Payload)
			}
			if !strings.Contains(event.Payload, "tool=bash") {
				t.Errorf("payload missing tool: %q", event.Payload)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected LOOP_UNKNOWN_STATUS event")
	}
}
