package agentic

import (
	"context"
	"strings"
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

type goalsMockHost struct {
	*noopHost
	committedText *string
}

func (h *goalsMockHost) CommitTextWithProfile(_ context.Context, _ models.Task, text string, _ *models.AgentProfile) {
	if h.committedText != nil {
		*h.committedText = text
	}
}

func TestProcessAgenticIteration_NoToolCallsUpdatesGoalProgress(t *testing.T) {
	committedText := ""
	host := &goalsMockHost{noopHost: &noopHost{}, committedText: &committedText}
	gw := &sequenceGateway{responses: []gateway.AIResponse{{Content: "[COMPLETED] a\nfinal response"}}}
	fakeStore := testutil.NewFakeStore()
	e := &Engine{
		config: Config{
			Gateway:       gw,
			Store:         fakeStore,
			MessageEditor: agentcontext.NewMessageEditor(wsession.NewMemoryCheckpointStore(), nil, nil),
		},
		host: host,
	}

	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123"}, ProjectID: "project-123", AgentID: "agent-123"}
	goalTracker := agentcontext.NewGoalTracker(task.ID, task.ProjectID)
	goalTracker.SetGoal(agentcontext.AgentGoal{SuccessCriteria: []string{"a"}})
	cm := agentcontext.NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, e.config.Gateway, task.AgentID, task.ID)
	messages := []gateway.PromptMessage{{Role: "user", Content: "do work"}}

	ctxBudget := agentruntime.NewContextBudgetGuard(60000, 0)
	respecAttempts := 0
	cont, result, report, _, err := e.processAgenticIteration(
		context.Background(),
		task,
		models.Project{},
		models.AgentProfile{},
		&messages,
		nil,
		nil, agenttools.NewToolExecutor(nil, t.TempDir(), nil, 0), agentruntime.NewIterationGuard(3), agentruntime.NewBudgetGuard(nil, task.ID), agentruntime.NewDeadlineGuard(context.Background()), ctxBudget,
		cm,
		goalTracker,
		nil, agenthooks.NewHookChain(), nil, agenttools.NewToolFailureTracker(0), nil,
		"task-123:0",
		0,
		&respecAttempts,
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("processAgenticIteration() error = %v", err)
	}
	if cont {
		t.Fatal("expected no-tool response to stop loop")
	}
	if !report || result.Status != agentruntime.LoopSuccessfulCompletion {
		t.Fatalf("result = %+v report = %v, want successful completion", result, report)
	}
	if !strings.Contains(committedText, "final response") {
		t.Fatalf("committed text = %q, want final response", committedText)
	}
	goal := goalTracker.Goal()
	if len(goal.CompletedCriteria) != 1 || goal.CompletedCriteria[0] != "a" {
		t.Fatalf("completed criteria = %v, want [a]", goal.CompletedCriteria)
	}
}
