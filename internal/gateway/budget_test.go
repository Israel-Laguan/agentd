package gateway

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/models"
)

func TestBudgetEnforcement_BlocksAfterCapReached(t *testing.T) {
	tracker := NewBudgetTracker(1000)
	provider := &fakeProvider{
		providerName: "mock",
		resp:         AIResponse{Content: "ok", ProviderUsed: "mock", TokenUsage: 800},
	}
	router := NewRouter(provider).WithBudget(tracker)

	// First request uses 800 of 1000 budget.
	_, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "first"}},
		TaskID:   "task-1",
	})
	if err != nil {
		t.Fatalf("first request error = %v", err)
	}
	if tracker.Usage("task-1") != 800 {
		t.Fatalf("usage = %d, want 800", tracker.Usage("task-1"))
	}

	// Second request uses another 300, pushing total to 1100 (over cap).
	provider.resp.TokenUsage = 300
	provider.calls = 0
	_, err = router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "second"}},
		TaskID:   "task-1",
	})
	if err != nil {
		t.Fatalf("second request error = %v (usage 1100 was set post-call, not pre-call)", err)
	}
	if tracker.Usage("task-1") != 1100 {
		t.Fatalf("usage = %d, want 1100", tracker.Usage("task-1"))
	}

	// Third request should be blocked: usage 1100 >= cap 1000.
	provider.calls = 0
	_, err = router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "third"}},
		TaskID:   "task-1",
	})
	if !errors.Is(err, models.ErrBudgetExceeded) {
		t.Fatalf("third request error = %v, want ErrBudgetExceeded", err)
	}
	if provider.calls != 0 {
		t.Fatalf("provider calls = %d, want 0 (should be blocked pre-call)", provider.calls)
	}
}

func TestBudgetEnforcement_IndependentTasks(t *testing.T) {
	tracker := NewBudgetTracker(1000)
	provider := &fakeProvider{
		providerName: "mock",
		resp:         AIResponse{Content: "ok", ProviderUsed: "mock", TokenUsage: 900},
	}
	router := NewRouter(provider).WithBudget(tracker)

	_, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "a"}},
		TaskID:   "task-A",
	})
	if err != nil {
		t.Fatalf("task-A error = %v", err)
	}

	_, err = router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "b"}},
		TaskID:   "task-B",
	})
	if err != nil {
		t.Fatalf("task-B error = %v, want nil (independent budget)", err)
	}
}

func TestBudgetEnforcement_EmptyTaskIDSkipsBudget(t *testing.T) {
	tracker := NewBudgetTracker(1)
	provider := &fakeProvider{
		providerName: "mock",
		resp:         AIResponse{Content: "ok", ProviderUsed: "mock", TokenUsage: 9999},
	}
	router := NewRouter(provider).WithBudget(tracker)

	_, err := router.Generate(context.Background(), AIRequest{
		Messages: []PromptMessage{{Role: "user", Content: "no task"}},
	})
	if err != nil {
		t.Fatalf("error = %v, want nil (no TaskID => budget skipped)", err)
	}
}

func TestBudgetTracker_Reset(t *testing.T) {
	tracker := NewBudgetTracker(100)
	tracker.Add("t1", 100)
	if err := tracker.Reserve("t1"); err == nil {
		t.Fatal("Reserve should fail at cap")
	}
	tracker.Reset("t1")
	if err := tracker.Reserve("t1"); err != nil {
		t.Fatalf("Reserve after Reset = %v", err)
	}
}
