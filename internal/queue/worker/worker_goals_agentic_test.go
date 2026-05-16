package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestBuildAgenticMessagesAddsGoalInstructionsOnlyWithGoal(t *testing.T) {
	w := &Worker{}
	messages := []gateway.PromptMessage{{Role: "user", Content: "do work"}}

	withoutGoal := w.buildAgenticMessages(messages, models.AgentProfile{})
	if strings.Contains(withoutGoal[0].Content, "[COMPLETED]") {
		t.Fatalf("goal instructions present without goal: %q", withoutGoal[0].Content)
	}

	withGoal := w.buildAgenticMessages(messages, models.AgentProfile{}, &AgentGoal{SuccessCriteria: []string{"a"}})
	if !strings.Contains(withGoal[0].Content, "[COMPLETED] criterion text") {
		t.Fatalf("missing completed marker instructions: %q", withGoal[0].Content)
	}
	if !strings.Contains(withGoal[0].Content, "[BLOCKED] criterion text") {
		t.Fatalf("missing blocked marker instructions: %q", withGoal[0].Content)
	}
}

func TestProcessAgenticIteration_NoToolCallsUpdatesGoalProgress(t *testing.T) {
	committedText := ""
	w := &Worker{
		store:   &mockCommitStore{text: &committedText},
		gateway: &sequenceGateway{responses: []gateway.AIResponse{{Content: "[COMPLETED] a\nfinal response"}}},
	}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123"}, ProjectID: "project-123", AgentID: "agent-123"}
	goalTracker := NewGoalTracker(nil, task.ID, task.ProjectID)
	goalTracker.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}})
	cm := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, w.gateway, task.AgentID, task.ID)
	messages := []gateway.PromptMessage{{Role: "user", Content: "do work"}}

	cont, err := w.processAgenticIteration(
		context.Background(),
		task,
		models.AgentProfile{},
		&messages,
		nil,
		nil,
		NewToolExecutor(nil, t.TempDir(), nil, 0),
		NewIterationGuard(3),
		NewBudgetGuard(nil, task.ID),
		NewDeadlineGuard(context.Background()),
		cm,
		goalTracker,
		NewHookChain(),
		nil,
	)
	if err != nil {
		t.Fatalf("processAgenticIteration() error = %v", err)
	}
	if cont {
		t.Fatal("expected no-tool response to stop loop")
	}
	if !strings.Contains(committedText, "final response") {
		t.Fatalf("committed text = %q, want final response", committedText)
	}
	goal := goalTracker.Goal()
	if len(goal.CompletedCriteria) != 1 || goal.CompletedCriteria[0] != "a" {
		t.Fatalf("completed criteria = %v, want [a]", goal.CompletedCriteria)
	}
}

func TestHandleGoalStalledPropagatesBlockError(t *testing.T) {
	blockErr := errors.New("block failed")
	w := &Worker{store: &mockCommitStore{blockErr: blockErr}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123", UpdatedAt: time.Now()}, ProjectID: "project-123"}
	gt := NewGoalTracker(nil, task.ID, task.ProjectID)
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}, TurnsActive: 11})

	if err := w.handleGoalStalled(context.Background(), task, gt); !errors.Is(err, blockErr) {
		t.Fatalf("handleGoalStalled() error = %v, want %v", err, blockErr)
	}
}
