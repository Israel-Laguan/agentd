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

func TestBuildSystemPromptContentAddsGoalInstructionsOnlyWithGoal(t *testing.T) {
	w := &Worker{}
	task := models.Task{Title: "t", Description: "d"}
	project := models.Project{}

	withoutGoal := w.buildSystemPromptContent(task, project, models.AgentProfile{})
	if strings.Contains(withoutGoal, "[COMPLETED]") {
		t.Fatalf("goal instructions present without goal: %q", withoutGoal)
	}

	withCriteria := models.Task{
		Title:           "t",
		Description:     "d",
		SuccessCriteria: []string{"a"},
	}
	withGoal := w.buildSystemPromptContent(withCriteria, project, models.AgentProfile{})
	if !strings.Contains(withGoal, "- a\n") {
		t.Fatalf("missing success criterion in prompt: %q", withGoal)
	}
	if !strings.Contains(withGoal, "[COMPLETED] criterion text") {
		t.Fatalf("missing completed marker instructions: %q", withGoal)
	}
	if !strings.Contains(withGoal, "[BLOCKED] criterion text") {
		t.Fatalf("missing blocked marker instructions: %q", withGoal)
	}
}

func TestProcessAgenticIteration_NoToolCallsUpdatesGoalProgress(t *testing.T) {
	committedText := ""
	w := &Worker{
		store:   &mockCommitStore{text: &committedText},
		gateway: &sequenceGateway{responses: []gateway.AIResponse{{Content: "[COMPLETED] a\nfinal response"}}},
	}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123"}, ProjectID: "project-123", AgentID: "agent-123"}
	goalTracker := NewGoalTracker(task.ID, task.ProjectID)
	goalTracker.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}})
	cm := NewContextManager(config.AgenticContextConfig{RollingThresholdTurns: 100}, w.gateway, task.AgentID, task.ID)
	messages := []gateway.PromptMessage{{Role: "user", Content: "do work"}}

	ctxBudget := NewContextBudgetGuard(60000, 0)
	cont, result, report, err := w.processAgenticIteration(
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
		ctxBudget,
		cm,
		goalTracker,
		NewHookChain(),
		nil,
		newToolFailureTracker(0),
		nil,
		"task-123:0",
		0,
	)
	if err != nil {
		t.Fatalf("processAgenticIteration() error = %v", err)
	}
	if cont {
		t.Fatal("expected no-tool response to stop loop")
	}
	if !report || result.Status != LoopSuccessfulCompletion {
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

func TestHandleGoalStalledPropagatesBlockError(t *testing.T) {
	blockErr := errors.New("block failed")
	w := &Worker{store: &mockCommitStore{blockErr: blockErr}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123", UpdatedAt: time.Now()}, ProjectID: "project-123"}
	gt := NewGoalTracker(task.ID, task.ProjectID)
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}, TurnsActive: DefaultStallThreshold + 1})

	if err := w.handleGoalStalled(context.Background(), task, gt); !errors.Is(err, blockErr) {
		t.Fatalf("handleGoalStalled() error = %v, want %v", err, blockErr)
	}
}

func TestHandleGoalStalled_RefreshesTaskUpdatedAt(t *testing.T) {
	freshAt := time.Now().Add(time.Hour)
	store := &goalStallRefreshedStore{freshUpdatedAt: freshAt}
	w := &Worker{store: store}
	staleAt := time.Now().Add(-time.Hour)
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123", UpdatedAt: staleAt}, ProjectID: "project-123"}
	gt := NewGoalTracker(task.ID, task.ProjectID)
	gt.SetGoal(AgentGoal{SuccessCriteria: []string{"a"}, TurnsActive: DefaultStallThreshold + 1})

	if err := w.handleGoalStalled(context.Background(), task, gt); err != nil {
		t.Fatalf("handleGoalStalled() error = %v", err)
	}
	if !store.blockCalled {
		t.Fatal("expected BlockTaskWithSubtasks to be called")
	}
	if !store.blockAt.Equal(freshAt) {
		t.Fatalf("BlockTaskWithSubtasks updatedAt = %v, want refreshed %v", store.blockAt, freshAt)
	}
}

type goalStallRefreshedStore struct {
	mockCommitStore
	freshUpdatedAt time.Time
	blockAt        time.Time
	blockCalled    bool
}

func (s *goalStallRefreshedStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: id, UpdatedAt: s.freshUpdatedAt}}, nil
}

func (s *goalStallRefreshedStore) BlockTaskWithSubtasks(_ context.Context, _ string, at time.Time, _ []models.DraftTask) (*models.Task, []models.Task, error) {
	s.blockCalled = true
	s.blockAt = at
	return nil, nil, nil
}
