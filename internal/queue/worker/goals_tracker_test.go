package worker

import (
	"context"
	"testing"
)

func TestGoalTracker_AfterTurn_StallDetection(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1", WithStallThreshold(3))
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"a", "b", "c"},
	})

	for i := 0; i < 3; i++ {
		if stalled := gt.AfterTurn(context.Background(), nil, nil); stalled {
			t.Fatalf("stalled too early at turn %d", i+1)
		}
	}
	if stalled := gt.AfterTurn(context.Background(), nil, nil); !stalled {
		t.Fatal("expected stall after 4 turns with no progress (threshold=3)")
	}
}

func TestGoalTracker_AfterTurn_DefaultThreshold(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1")
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}})

	for i := 0; i < DefaultStallThreshold; i++ {
		if stalled := gt.AfterTurn(context.Background(), nil, nil); stalled {
			t.Fatalf("stalled too early at turn %d", i+1)
		}
	}
	if stalled := gt.AfterTurn(context.Background(), nil, nil); !stalled {
		t.Fatalf("expected stall after %d turns with no progress", DefaultStallThreshold+1)
	}
}

func TestGoalTracker_AfterTurn_ProgressPreventsStall(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1", WithStallThreshold(3))
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"a", "b", "c"},
	})

	for i := 0; i < 3; i++ {
		gt.AfterTurn(context.Background(), nil, nil)
	}
	if stalled := gt.AfterTurn(context.Background(), []string{"a"}, nil); stalled {
		t.Fatal("should not stall with 33% progress")
	}
}

func TestGoalTracker_AfterTurn_IgnoresUnknownCriteria(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1")
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a", "b"}})

	if stalled := gt.AfterTurn(context.Background(), []string{"x"}, []string{"y"}); stalled {
		t.Fatal("should not stall on first turn with unknown criteria")
	}
	goal := gt.Goal()
	if len(goal.CompletedCriteria) != 0 {
		t.Fatalf("completed = %v, want []", goal.CompletedCriteria)
	}
	if len(goal.BlockedCriteria) != 0 {
		t.Fatalf("blocked = %v, want []", goal.BlockedCriteria)
	}
}

func TestGoalTracker_AfterTurn_NilGoal(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1")
	if stalled := gt.AfterTurn(context.Background(), []string{"a"}, nil); stalled {
		t.Fatal("should not stall with nil goal")
	}
}

func TestGoalTracker_Goal_ReturnsSnapshot(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1")
	gt.SetGoal(AgentGoal{
		SuccessCriteria: []string{"a"},
	})
	snap := gt.Goal()
	snap.SuccessCriteria = append(snap.SuccessCriteria, "mutated")
	original := gt.Goal()
	if len(original.SuccessCriteria) != 1 {
		t.Fatal("mutation leaked through snapshot")
	}
}

func TestGoalTracker_SetGoal_IsolatesMutation(t *testing.T) {
	gt := NewGoalTracker("task-1", "project-1")
	goal := AgentGoal{SuccessCriteria: []string{"a"}}
	gt.SetGoal(goal)
	goal.SuccessCriteria = append(goal.SuccessCriteria, "mutated")
	stored := gt.Goal()
	if len(stored.SuccessCriteria) != 1 || stored.SuccessCriteria[0] != "a" {
		t.Fatalf("mutation leaked through SetGoal: %v", stored.SuccessCriteria)
	}
}

// -- CriteriaUpdater persistence tests --

type fakeCriteriaStore struct {
	calls [][]string
	err   error
}

func (f *fakeCriteriaStore) UpdateCriteriaMet(_ context.Context, _ string, met []string) error {
	f.calls = append(f.calls, append([]string(nil), met...))
	return f.err
}

func TestGoalTracker_AfterTurn_PersistsCriteriaOnCompletion(t *testing.T) {
	store := &fakeCriteriaStore{}
	gt := NewGoalTracker("task-1", "project-1", WithCriteriaStore(store))
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a", "b"}})

	gt.AfterTurn(context.Background(), []string{"a"}, nil)

	if len(store.calls) != 1 {
		t.Fatalf("UpdateCriteriaMet called %d times, want 1", len(store.calls))
	}
	if len(store.calls[0]) != 1 || store.calls[0][0] != "a" {
		t.Fatalf("UpdateCriteriaMet got %v, want [a]", store.calls[0])
	}
}

func TestGoalTracker_AfterTurn_DoesNotPersistWhenNothingCompleted(t *testing.T) {
	store := &fakeCriteriaStore{}
	gt := NewGoalTracker("task-1", "project-1", WithCriteriaStore(store))
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a", "b"}})

	gt.AfterTurn(context.Background(), nil, nil)

	if len(store.calls) != 0 {
		t.Fatalf("UpdateCriteriaMet called %d times, want 0", len(store.calls))
	}
}

func TestGoalTracker_AfterTurn_AccumulatesCriteria(t *testing.T) {
	store := &fakeCriteriaStore{}
	gt := NewGoalTracker("task-1", "project-1", WithCriteriaStore(store))
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a", "b"}})

	gt.AfterTurn(context.Background(), []string{"a"}, nil)
	gt.AfterTurn(context.Background(), []string{"b"}, nil)

	if len(store.calls) != 2 {
		t.Fatalf("UpdateCriteriaMet called %d times, want 2", len(store.calls))
	}
	// Second call should contain both criteria (accumulated).
	if len(store.calls[1]) != 2 {
		t.Fatalf("second UpdateCriteriaMet got %v, want [a b]", store.calls[1])
	}
}

func TestGoalTracker_AfterTurn_PersistErrorIsNonFatal(t *testing.T) {
	store := &fakeCriteriaStore{err: context.DeadlineExceeded}
	gt := NewGoalTracker("task-1", "project-1", WithCriteriaStore(store), WithStallThreshold(100))
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}})

	// Should not panic or return true (stalled) due to the store error.
	stalled := gt.AfterTurn(context.Background(), []string{"a"}, nil)
	if stalled {
		t.Fatal("persist error should not cause stall")
	}
}
