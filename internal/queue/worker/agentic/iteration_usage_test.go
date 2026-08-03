package agentic

import (
	"context"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// usageSpyHost is a Host implementation that records the arguments passed to
// RecordTaskTokenUsage so tests can assert on the forwarded token usage and
// cache details.
type usageSpyHost struct {
	*noopHost
	calls []usageRecord
}

type usageRecord struct {
	tokens  int
	details gateway.UsageDetails
}

func (h *usageSpyHost) RecordTaskTokenUsage(_ context.Context, _ models.Task, tokens int, details gateway.UsageDetails) {
	h.calls = append(h.calls, usageRecord{tokens: tokens, details: details})
}

func newUsageTestEngine(t *testing.T, host *usageSpyHost, gw *sequenceGateway, taskID string) (*Engine, models.Task, *[]gateway.PromptMessage, context.Context) {
	t.Helper()
	e := &Engine{
		config: Config{
			Gateway:       gw,
			Store:         testutil.NewFakeStore(),
			MessageEditor: agentcontext.NewMessageEditor(wsession.NewMemoryCheckpointStore(), nil, nil),
		},
		host: host,
	}
	task := models.Task{BaseEntity: models.BaseEntity{ID: taskID}, AgentID: "agent-1"}
	messages := []gateway.PromptMessage{{Role: "user", Content: "do work"}}
	ctx := context.Background()
	return e, task, &messages, ctx
}

func newUsageTestGuards(t *testing.T, task models.Task, messages *[]gateway.PromptMessage) (
	*agentruntime.IterationGuard, *agentruntime.BudgetGuard, *agentruntime.DeadlineGuard,
	*agentruntime.ContextBudgetGuard, *agentcontext.ContextManager, *agentcontext.GoalTracker,
	*SessionManager, *int,
) {
	t.Helper()
	cm := agentcontext.NewContextManager(
		config.AgenticContextConfig{RollingThresholdTurns: 100},
		nil, task.AgentID, task.ID,
	)
	goalTracker := agentcontext.NewGoalTracker(task.ID, "")
	ctxBudget := agentruntime.NewContextBudgetGuard(60000, 0)
	respecAttempts := 0
	return agentruntime.NewIterationGuard(3),
		agentruntime.NewBudgetGuard(nil, task.ID),
		agentruntime.NewDeadlineGuard(context.Background()),
		ctxBudget,
		cm,
		goalTracker,
		NewSessionManager(task.ID, "do work", nil),
		&respecAttempts
}

// TestProcessAgenticIteration_ForwardsNonZeroUsageDetails verifies that when the
// gateway response carries non-zero cached and cache-write token counts, the
// iteration loop forwards the exact normalized UsageDetails to the host's
// RecordTaskTokenUsage without modification.
func TestProcessAgenticIteration_ForwardsNonZeroUsageDetails(t *testing.T) {
	t.Parallel()
	host := &usageSpyHost{noopHost: &noopHost{}}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{
			Content:    "[COMPLETED] done",
			TokenUsage: 42,
			UsageDetails: &gateway.UsageDetails{
				CachedTokens:     17,
				CacheWriteTokens: 9,
			},
		},
	}}
	e, task, messages, ctx := newUsageTestEngine(t, host, gw, "task-usage")
	it, bb, dg, cbg, cm, gt, sm, ra := newUsageTestGuards(t, task, messages)

	cont, _, _, _, err := e.processAgenticIteration(
		ctx, task, models.Project{}, models.AgentProfile{},
		messages, nil, nil,
		agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0),
		it, bb, dg, cbg, cm, gt, sm,
		agenthooks.NewHookChain(), nil,
		agenttools.NewToolFailureTracker(0), nil,
		"turn-0", 0, ra,
		nil, nil, nil, nil,
	)

	if err != nil {
		t.Fatalf("processAgenticIteration error: %v", err)
	}
	if cont {
		t.Fatal("expected loop to stop on completion")
	}
	if len(host.calls) != 1 {
		t.Fatalf("RecordTaskTokenUsage calls = %d, want 1", len(host.calls))
	}
	if host.calls[0].tokens != 42 {
		t.Errorf("tokens = %d, want 42", host.calls[0].tokens)
	}
	if host.calls[0].details.CachedTokens != 17 {
		t.Errorf("CachedTokens = %d, want 17", host.calls[0].details.CachedTokens)
	}
	if host.calls[0].details.CacheWriteTokens != 9 {
		t.Errorf("CacheWriteTokens = %d, want 9", host.calls[0].details.CacheWriteTokens)
	}
}

// TestProcessAgenticIteration_ForwardsZeroUsageDetailsWhenNil verifies that when
// the gateway response has a nil UsageDetails pointer, the iteration loop
// forwards a zero-value UsageDetails (rather than panicking or omitting the call).
func TestProcessAgenticIteration_ForwardsZeroUsageDetailsWhenNil(t *testing.T) {
	t.Parallel()
	host := &usageSpyHost{noopHost: &noopHost{}}
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{
			Content:      "[COMPLETED] done",
			TokenUsage:   15,
			UsageDetails: nil,
		},
	}}
	e, task, messages, ctx := newUsageTestEngine(t, host, gw, "task-nil-usage")
	it, bb, dg, cbg, cm, gt, sm, ra := newUsageTestGuards(t, task, messages)

	_, _, _, _, err := e.processAgenticIteration(
		ctx, task, models.Project{}, models.AgentProfile{},
		messages, nil, nil,
		agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0),
		it, bb, dg, cbg, cm, gt, sm,
		agenthooks.NewHookChain(), nil,
		agenttools.NewToolFailureTracker(0), nil,
		"turn-0", 0, ra,
		nil, nil, nil, nil,
	)

	if err != nil {
		t.Fatalf("processAgenticIteration error: %v", err)
	}
	if len(host.calls) != 1 {
		t.Fatalf("RecordTaskTokenUsage calls = %d, want 1", len(host.calls))
	}
	if host.calls[0].tokens != 15 {
		t.Errorf("tokens = %d, want 15", host.calls[0].tokens)
	}
	if host.calls[0].details.CachedTokens != 0 || host.calls[0].details.CacheWriteTokens != 0 {
		t.Errorf("details = %+v, want zero-value UsageDetails when resp.UsageDetails is nil",
			host.calls[0].details)
	}
}
