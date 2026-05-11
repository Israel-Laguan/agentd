package worker

import (
	"context"
	"testing"
	"time"

	"agentd/internal/gateway"
)

func TestIterationGuard_BeforeIteration_AllowsFirstCall(t *testing.T) {
	g := NewIterationGuard(3)
	if err := g.BeforeIteration(); err != nil {
		t.Fatalf("expected no error on first iteration, got %v", err)
	}
}

func TestIterationGuard_BeforeIteration_BlocksAfterExceeded(t *testing.T) {
	g := NewIterationGuard(2)
	// First iteration
	g.AfterIteration(true)
	// Second iteration - exceeded but allows final call
	g.AfterIteration(true)
	// After final call, should block
	g.ResetAllowFinal()
	if err := g.BeforeIteration(); err == nil {
		t.Fatal("expected error after iteration limit exceeded and final call used")
	}
}

func TestIterationGuard_AfterIteration_TracksCount(t *testing.T) {
	g := NewIterationGuard(3)
	g.AfterIteration(true)
	g.AfterIteration(true)
	if g.IsExceeded() {
		t.Fatal("should not be exceeded yet")
	}
	g.AfterIteration(true)
	if !g.IsExceeded() {
		t.Fatal("should be exceeded after 3 iterations")
	}
}

func TestIterationGuard_ShouldInjectFinalMessage(t *testing.T) {
	g := NewIterationGuard(2)
	g.AfterIteration(true)
	g.AfterIteration(true)
	if !g.ShouldInjectFinalMessage() {
		t.Fatal("should inject final message after exceeded")
	}
	msg := g.FinalMessage()
	if msg.Role != "user" {
		t.Fatalf("expected role user, got %s", msg.Role)
	}
	if msg.Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestIterationGuard_ResetAllowFinal(t *testing.T) {
	g := NewIterationGuard(2)
	g.AfterIteration(true)
	g.AfterIteration(true)
	if !g.ShouldInjectFinalMessage() {
		t.Fatal("should allow final message")
	}
	g.ResetAllowFinal()
	if g.ShouldInjectFinalMessage() {
		t.Fatal("should not allow final message after reset")
	}
}

func TestIterationGuard_NoToolCalls_DoesNotCount(t *testing.T) {
	g := NewIterationGuard(2)
	g.AfterIteration(false)
	g.AfterIteration(false)
	if g.IsExceeded() {
		t.Fatal("should not be exceeded when no tool calls")
	}
}

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

func TestDeadlineGuard_BeforeIteration_FutureDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Hour))
	defer cancel()
	g := NewDeadlineGuard(ctx)
	if err := g.BeforeIteration(); err != nil {
		t.Fatalf("expected no error with future deadline, got %v", err)
	}
}

func TestDeadlineGuard_BeforeIteration_ExpiredDeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()
	g := NewDeadlineGuard(ctx)
	if err := g.BeforeIteration(); err == nil {
		t.Fatal("expected error with expired deadline")
	}
}

func TestDeadlineGuard_BeforeIteration_AlreadyExpired(t *testing.T) {
	deadline := time.Now().Add(-1 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	// Wait for the deadline to definitely be in the past
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Millisecond)
	}
	g := NewDeadlineGuard(ctx)
	if err := g.BeforeIteration(); err == nil {
		t.Fatal("expected error when deadline already expired")
	}
}

func TestDeadlineGuard_Remaining(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(30*time.Minute))
	defer cancel()
	g := NewDeadlineGuard(ctx)
	remaining := g.Remaining()
	if remaining <= 0 {
		t.Fatalf("expected positive remaining time, got %v", remaining)
	}
	if remaining > 35*time.Minute || remaining < 25*time.Minute {
		t.Fatalf("expected remaining around 30 minutes, got %v", remaining)
	}
}

func TestDeadlineGuard_Deadline(t *testing.T) {
	deadline := time.Now().Add(1 * time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	g := NewDeadlineGuard(ctx)
	if !g.Deadline().Equal(deadline) {
		t.Fatalf("expected deadline %v, got %v", deadline, g.Deadline())
	}
}

func TestIterationCapExceeded_DeterministicOutcome(t *testing.T) {
	g := NewIterationGuard(1)
	g.AfterIteration(true)
	if !g.IsExceeded() {
		t.Fatal("should be exceeded")
	}
	if !g.ShouldInjectFinalMessage() {
		t.Fatal("should allow final message")
	}
	msg := g.FinalMessage()
	if msg.Role != "user" || msg.Content == "" {
		t.Fatal("should have valid final message")
	}
	// After injecting final message and resetting, should block
	g.ResetAllowFinal()
	err := g.BeforeIteration()
	if err == nil {
		t.Fatal("should return iteration limit error after final call")
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

func TestDeadlineExpiredBeforeSecondIteration(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(1*time.Millisecond))
	time.Sleep(10 * time.Millisecond)
	cancel()
	g := NewDeadlineGuard(ctx)
	err := g.BeforeIteration()
	if err == nil {
		t.Fatal("expected error when deadline expired")
	}
}
