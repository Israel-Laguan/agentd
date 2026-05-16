package worker

import (
	"reflect"
	"testing"

	"agentd/internal/models"
)

func TestProgressRatio_NoCriteria(t *testing.T) {
	g := AgentGoal{}
	if got := g.ProgressRatio(); got != 0 {
		t.Fatalf("ProgressRatio() = %v, want 0", got)
	}
}

func TestProgressRatio_AllCompleted(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a", "b"},
	}
	if got := g.ProgressRatio(); got != 1.0 {
		t.Fatalf("ProgressRatio() = %v, want 1.0", got)
	}
}

func TestProgressRatio_Partial(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b", "c", "d"},
		CompletedCriteria: []string{"a"},
	}
	if got := g.ProgressRatio(); got != 0.25 {
		t.Fatalf("ProgressRatio() = %v, want 0.25", got)
	}
}

func TestIsStalled_BelowThreshold(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a", "b"},
		TurnsActive:     5,
	}
	if g.IsStalled(10) {
		t.Fatal("should not be stalled when TurnsActive <= threshold")
	}
}

func TestIsStalled_AboveThresholdNoProgress(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a", "b", "c"},
		TurnsActive:     15,
	}
	if !g.IsStalled(10) {
		t.Fatal("should be stalled when TurnsActive > threshold and progress < 10%")
	}
}

func TestIsStalled_AboveThresholdWithProgress(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a"},
		TurnsActive:       15,
	}
	if g.IsStalled(10) {
		t.Fatal("should not be stalled when progress >= 10%")
	}
}

func TestMarkCompleted_Deduplicates(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a"},
	}
	g.MarkCompleted([]string{"a", "b", "b"})
	if !reflect.DeepEqual(g.CompletedCriteria, []string{"a", "b"}) {
		t.Fatalf("completed = %v, want [a b]", g.CompletedCriteria)
	}
}

func TestMarkCompleted_SkipsEmpty(t *testing.T) {
	g := AgentGoal{SuccessCriteria: []string{"a"}}
	g.MarkCompleted([]string{"", "  ", "a"})
	if !reflect.DeepEqual(g.CompletedCriteria, []string{"a"}) {
		t.Fatalf("completed = %v, want [a]", g.CompletedCriteria)
	}
}

func TestMarkCompleted_IgnoresUnknownCriteria(t *testing.T) {
	g := AgentGoal{SuccessCriteria: []string{"a", "b"}}
	g.MarkCompleted([]string{"x", "y"})
	if len(g.CompletedCriteria) != 0 {
		t.Fatalf("completed = %v, want []", g.CompletedCriteria)
	}
	if got := g.ProgressRatio(); got != 0 {
		t.Fatalf("ProgressRatio() = %v, want 0", got)
	}
}

func TestMarkCompleted_RemovesFromBlocked(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a", "b"},
		BlockedCriteria: []string{"a", "b"},
	}
	g.MarkCompleted([]string{"a"})
	if !reflect.DeepEqual(g.BlockedCriteria, []string{"b"}) {
		t.Fatalf("blocked = %v, want [b]", g.BlockedCriteria)
	}
}

func TestMarkBlocked_ExcludesCompleted(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria:   []string{"a", "b"},
		CompletedCriteria: []string{"a"},
	}
	g.MarkBlocked([]string{"a", "b"})
	if !reflect.DeepEqual(g.BlockedCriteria, []string{"b"}) {
		t.Fatalf("blocked = %v, want [b]", g.BlockedCriteria)
	}
}

func TestMarkBlocked_Deduplicates(t *testing.T) {
	g := AgentGoal{
		SuccessCriteria: []string{"a"},
		BlockedCriteria: []string{"a"},
	}
	g.MarkBlocked([]string{"a"})
	if !reflect.DeepEqual(g.BlockedCriteria, []string{"a"}) {
		t.Fatalf("blocked = %v, want [a]", g.BlockedCriteria)
	}
}

func TestMarkBlocked_IgnoresUnknownCriteria(t *testing.T) {
	g := AgentGoal{SuccessCriteria: []string{"a", "b"}}
	g.MarkBlocked([]string{"x", "y"})
	if len(g.BlockedCriteria) != 0 {
		t.Fatalf("blocked = %v, want []", g.BlockedCriteria)
	}
}

func TestGoalFromTask_NoCriteria(t *testing.T) {
	task := models.Task{Description: "do stuff"}
	if g := GoalFromTask(task); g != nil {
		t.Fatal("expected nil goal when no success criteria")
	}
}

func TestGoalFromTask_NormalizesCriteria(t *testing.T) {
	task := models.Task{
		Description:     "do stuff",
		SuccessCriteria: []string{" pass tests ", "", "lint clean", "pass tests", "  "},
	}
	g := GoalFromTask(task)
	if g == nil {
		t.Fatal("expected non-nil goal")
	}
	if !reflect.DeepEqual(g.SuccessCriteria, []string{"pass tests", "lint clean"}) {
		t.Fatalf("criteria = %v, want [pass tests lint clean]", g.SuccessCriteria)
	}
}

func TestGoalFromTask_BlankCriteriaOnly(t *testing.T) {
	task := models.Task{SuccessCriteria: []string{"", "  "}}
	if g := GoalFromTask(task); g != nil {
		t.Fatalf("goal = %#v, want nil", g)
	}
}
