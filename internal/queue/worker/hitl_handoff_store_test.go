package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// faultyHITLStore fails the store calls a test names, so a handoff or a HITL
// check can be driven down its failure path.
type faultyHITLStore struct {
	*testutil.FakeKanbanStore
	failBlock bool
	failReads bool
}

var errStoreDown = errors.New("store unavailable")

func (s *faultyHITLStore) BlockTaskWithSubtasksAndComments(
	ctx context.Context, taskID string, expectedUpdatedAt time.Time,
	subtasks []models.DraftTask, comments []models.Comment,
) (*models.Task, []models.Task, error) {
	if s.failBlock {
		return nil, nil, errStoreDown
	}
	return s.FakeKanbanStore.BlockTaskWithSubtasksAndComments(ctx, taskID, expectedUpdatedAt, subtasks, comments)
}

func (s *faultyHITLStore) GetTask(ctx context.Context, id string) (*models.Task, error) {
	if s.failReads {
		return nil, errStoreDown
	}
	return s.FakeKanbanStore.GetTask(ctx, id)
}

func (s *faultyHITLStore) ListChildTasks(ctx context.Context, parentID string) ([]models.Task, error) {
	if s.failReads {
		return nil, errStoreDown
	}
	return s.FakeKanbanStore.ListChildTasks(ctx, parentID)
}

func questionComments(t *testing.T, store *testutil.FakeKanbanStore, taskID string) int {
	t.Helper()
	comments, err := store.ListComments(context.Background(), taskID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	n := 0
	for _, c := range comments {
		if strings.HasPrefix(c.Body, hitlElicitationQuestionsPrefix) {
			n++
		}
	}
	return n
}

// The question is recorded in the same transaction that blocks the task and
// creates the clarification subtask, so a failed handoff leaves no question
// that nobody was asked.
func TestAskHumanFailedHandoffRecordsNoQuestion(t *testing.T) {
	t.Parallel()
	fake, task, _, _ := needsHumanFixture(t)
	store := &faultyHITLStore{FakeKanbanStore: fake, failBlock: true}
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())

	if err := w.AskHuman(context.Background(), task, "Which format?", nil); err == nil {
		t.Fatal("AskHuman() = nil, want the handoff error")
	}
	if n := questionComments(t, fake, task.ID); n != 0 {
		t.Fatalf("question comments = %d, want 0 after a failed handoff", n)
	}
}

func TestAskHumanRecordsQuestionWithTheHandoff(t *testing.T) {
	t.Parallel()
	fake, task, _, _ := needsHumanFixture(t)
	w := NewWorker(fake, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())

	if err := w.AskHuman(context.Background(), task, "Which format?", nil); err != nil {
		t.Fatalf("AskHuman() = %v", err)
	}
	if n := questionComments(t, fake, task.ID); n != 1 {
		t.Fatalf("question comments = %d, want 1", n)
	}
	if clarifications, _ := clarificationChildren(t, fake, task.ID); clarifications != 1 {
		t.Fatalf("clarification subtasks = %d, want 1", clarifications)
	}
}

// When neither the task nor its children can be read, the HITL check cannot
// tell, so it must not let the caller fail a task that may be waiting on a person.
func TestTaskBlockedOnHumanInconclusiveReadsKeepTheTask(t *testing.T) {
	t.Parallel()
	fake, task, _, _ := needsHumanFixture(t)
	store := &faultyHITLStore{FakeKanbanStore: fake, failReads: true}
	w := NewWorker(store, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())

	if !w.taskBlockedOnHuman(context.Background(), task) {
		t.Fatal("taskBlockedOnHuman() = false on unreadable state, want true (inconclusive keeps the task)")
	}
}

func TestTaskBlockedOnHumanReadableAndNotBlocked(t *testing.T) {
	t.Parallel()
	fake, task, _, _ := needsHumanFixture(t)
	w := NewWorker(fake, nil, &routingTestSandbox{}, nil, &legacyHandoffSink{}, legacyCapOptions())

	if w.taskBlockedOnHuman(context.Background(), task) {
		t.Fatal("taskBlockedOnHuman() = true for a READY task with no clarification")
	}
}
