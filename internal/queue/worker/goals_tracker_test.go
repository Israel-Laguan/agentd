package worker

import (
	"context"
	"testing"
)

func TestGoalTracker_AfterTurn_StallDetection(t *testing.T) {
	gt := NewGoalTracker(nil, "task-1", "project-1", WithStallThreshold(3))
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
	gt := NewGoalTracker(nil, "task-1", "project-1")
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
	gt := NewGoalTracker(nil, "task-1", "project-1", WithStallThreshold(3))
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
	gt := NewGoalTracker(nil, "task-1", "project-1")
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
	gt := NewGoalTracker(nil, "task-1", "project-1")
	if stalled := gt.AfterTurn(context.Background(), []string{"a"}, nil); stalled {
		t.Fatal("should not stall with nil goal")
	}
}

func TestGoalTracker_Goal_ReturnsSnapshot(t *testing.T) {
	gt := NewGoalTracker(nil, "task-1", "project-1")
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
