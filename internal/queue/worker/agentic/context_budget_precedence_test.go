package agentic

import (
	"context"
	"strings"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// A working zone over the context budget is cut with the configured
// strategy (gateway.truncation.strategy); the task goes on. Before, the guard
// failed the task on the raw size, so the cut never ran in a live loop.
func TestPrepareAgenticIteration_CutsWorkingZoneBeforeExhaustion(t *testing.T) {
	cm, guard := precedenceContext(gateway.HeadTailStrategy{HeadRatio: 0.8})
	big := strings.Repeat("h", 500) + strings.Repeat("t", 500)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: big},
	}

	exhausted, err := precedenceEngine().prepareAgenticIteration(
		context.Background(), &messages, agentruntime.NewIterationGuard(10), cm, guard, models.Task{})

	if err != nil {
		t.Fatalf("prepareAgenticIteration: %v", err)
	}
	if exhausted {
		t.Fatalf("task failed as exhausted with %d chars; the working zone should have been cut to fit %d",
			agentcontext.TotalChars(messages), cm.TotalBudget())
	}
	cut := messages[len(messages)-1].Content
	if heads, tails := strings.Count(cut, "h"), strings.Count(cut, "t"); heads <= 2*tails {
		t.Fatalf("working message not cut head_tail 0.8: %d h vs %d t", heads, tails)
	}
}

// The anchor (system prompt, task, preferences) is never cut, so an anchor
// over the budget still ends the task.
func TestPrepareAgenticIteration_AnchorOverBudgetStillExhausts(t *testing.T) {
	cm, guard := precedenceContext(nil)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: strings.Repeat("s", 500)},
		{Role: "user", Content: "task"},
	}

	exhausted, err := precedenceEngine().prepareAgenticIteration(
		context.Background(), &messages, agentruntime.NewIterationGuard(10), cm, guard, models.Task{})

	if err != nil {
		t.Fatalf("prepareAgenticIteration: %v", err)
	}
	if !exhausted {
		t.Fatal("an anchor over the budget was not reported as exhausted")
	}
}

func precedenceContext(strategy gateway.TruncationStrategy) (*agentcontext.ContextManager, *agentruntime.ContextBudgetGuard) {
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{
		AnchorBudget: 100, WorkingBudget: 200, CompressedBudget: 100,
		RollingThresholdTurns: 100, KeepRecentTurns: 1,
	}, nil, "agent", "task")
	cm.SetTruncationStrategy(strategy)
	return cm, agentruntime.NewContextBudgetGuard(cm.TotalBudget(), 0)
}

func precedenceEngine() *Engine {
	return &Engine{config: Config{Store: testutil.NewFakeStore()}}
}

// The iteration-limit message is part of the budget, not appended after it.
func TestPrepareAgenticIteration_FinalMessageStaysWithinBudget(t *testing.T) {
	cm, guard := precedenceContext(nil)
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: strings.Repeat("x", 1000)},
	}
	iterations := agentruntime.NewIterationGuard(1)
	iterations.AfterIteration(true)

	exhausted, err := precedenceEngine().prepareAgenticIteration(
		context.Background(), &messages, iterations, cm, guard, models.Task{})

	if err != nil || exhausted {
		t.Fatalf("prepareAgenticIteration: exhausted=%v err=%v", exhausted, err)
	}
	if got := agentcontext.TotalChars(messages); got > cm.TotalBudget() {
		t.Fatalf("prompt is %d chars after the final message, over the %d budget", got, cm.TotalBudget())
	}
	if last := messages[len(messages)-1]; last.Content != agentruntime.IterationExceededMessage {
		t.Fatalf("last message = %q, want the iteration-limit message", last.Content)
	}
}
