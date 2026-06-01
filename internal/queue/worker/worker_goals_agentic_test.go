package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	agentcontext "agentd/internal/agent/context"
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

func TestHandleGoalStalledPropagatesBlockError(t *testing.T) {
	blockErr := errors.New("block failed")
	w := &Worker{store: &mockCommitStore{blockErr: blockErr}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "task-123", UpdatedAt: time.Now()}, ProjectID: "project-123"}
	gt := agentcontext.NewGoalTracker(task.ID, task.ProjectID)
	gt.SetGoal(agentcontext.AgentGoal{SuccessCriteria: []string{"a"}, TurnsActive: agentcontext.DefaultStallThreshold + 1})

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
	gt := agentcontext.NewGoalTracker(task.ID, task.ProjectID)
	gt.SetGoal(agentcontext.AgentGoal{SuccessCriteria: []string{"a"}, TurnsActive: agentcontext.DefaultStallThreshold + 1})

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
