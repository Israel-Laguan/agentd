package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

type spyCanceller struct {
	calls []string
}

func (s *spyCanceller) Cancel(taskID string) bool {
	s.calls = append(s.calls, taskID)
	return true
}

func TestAddComment_HumanAuthorTriggersCanceller(t *testing.T) {
	store := newTestStore(t)
	spy := &spyCanceller{}
	store = store.WithCanceller(spy)

	parent := seedTestTask(t, store, "cancel-target", models.TaskStateRunning)

	err := store.AddComment(context.Background(), models.Comment{
		TaskID: parent.ID,
		Author: "Human",
		Body:   "stop please",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0] != parent.ID {
		t.Fatalf("Cancel calls = %v, want [%s]", spy.calls, parent.ID)
	}

	task, err := store.GetTask(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.State != models.TaskStateInConsideration {
		t.Fatalf("state = %s, want IN_CONSIDERATION", task.State)
	}
}

func TestAddComment_SystemAuthorDoesNotTriggerCanceller(t *testing.T) {
	store := newTestStore(t)
	spy := &spyCanceller{}
	store = store.WithCanceller(spy)

	parent := seedTestTask(t, store, "system-comment", models.TaskStateRunning)

	err := store.AddComment(context.Background(), models.Comment{
		TaskID: parent.ID,
		Author: "System",
		Body:   "internal note",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("Cancel calls = %v, want none", spy.calls)
	}
}

func TestAddComment_NilCancellerDoesNotPanic(t *testing.T) {
	store := newTestStore(t)
	parent := seedTestTask(t, store, "nil-canceller", models.TaskStateRunning)

	err := store.AddComment(context.Background(), models.Comment{
		TaskID: parent.ID,
		Author: "Human",
		Body:   "should not panic",
	})
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
}
