package runaudit

import (
	"testing"

	"agentd/internal/gateway"
)

func TestBudgetGuard_BeforeCall_NilTracker(t *testing.T) {
	g := NewBudgetGuard(nil, "task-1")
	if err := g.BeforeCall(); err != nil {
		t.Fatalf("expected no error with nil tracker, got %v", err)
	}
}

func TestBudgetGuard_BeforeCall_EmptyTaskID(t *testing.T) {
	tracker := gateway.NewBudgetTracker(1000)
	g := NewBudgetGuard(tracker, "")
	if err := g.BeforeCall(); err != nil {
		t.Fatalf("expected no error with empty task ID, got %v", err)
	}
}

func TestBudgetGuard_BeforeCall_ReservesBudget(t *testing.T) {
	tracker := gateway.NewBudgetTracker(1000)
	g := NewBudgetGuard(tracker, "task-1")
	if err := g.BeforeCall(); err != nil {
		t.Fatalf("expected no error on first reserve, got %v", err)
	}
}

func TestBudgetGuard_BeforeCall_ExceedsBudget(t *testing.T) {
	tracker := gateway.NewBudgetTracker(100)
	g := NewBudgetGuard(tracker, "task-1")
	tracker.Add("task-1", 100)
	err := g.BeforeCall()
	if err == nil {
		t.Fatal("expected error when budget exceeded")
	}
	if !g.IsBudgetExceeded(err) {
		t.Fatal("expected ErrBudgetExceeded")
	}
}

func TestBudgetGuard_AfterCall_AccumulatesUsage(t *testing.T) {
	tracker := gateway.NewBudgetTracker(1000)
	g := NewBudgetGuard(tracker, "task-1")
	g.AfterCall(100)
	g.AfterCall(200)
	if g.Usage() != 300 {
		t.Fatalf("expected usage 300, got %d", g.Usage())
	}
}

func TestBudgetGuard_AfterCall_ZeroTokens(t *testing.T) {
	tracker := gateway.NewBudgetTracker(1000)
	g := NewBudgetGuard(tracker, "task-1")
	g.AfterCall(0)
	if g.Usage() != 0 {
		t.Fatalf("expected usage 0, got %d", g.Usage())
	}
}

func TestBudgetExceeded_ControlledError(t *testing.T) {
	tracker := gateway.NewBudgetTracker(100)
	tracker.Add("task-1", 100)
	g := NewBudgetGuard(tracker, "task-1")
	err := g.BeforeCall()
	if err == nil {
		t.Fatal("expected error when budget exceeded")
	}
	if !g.IsBudgetExceeded(err) {
		t.Fatal("expected ErrBudgetExceeded")
	}
}
