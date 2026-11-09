package testutil

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) UpdateTaskPatch(_ context.Context, id string, expectedUpdatedAt time.Time, state *models.TaskState, description *string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	if !t.UpdatedAt.Equal(expectedUpdatedAt) {
		return nil, models.ErrStateConflict
	}
	if state != nil && !t.State.CanTransitionTo(*state) {
		return nil, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, t.State, *state)
	}
	ts := now()
	if state != nil {
		t.State = *state
		t.OSProcessID = nil
		t.CompletedAt = terminalCompletedAt(*state, ts)
	}
	if description != nil {
		t.Description = *description
	}
	t.UpdatedAt = ts
	s.tasks[id] = t
	if state != nil && (*state == models.TaskStateCompleted || *state == models.TaskStateFailed) {
		s.unblockBlockedParentsLocked(id)
	}
	return &t, nil
}

func (s *FakeKanbanStore) UpdateTaskCriteria(_ context.Context, id string, expected time.Time, criteria []string, golden []models.GoldenCriterion) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	if !t.UpdatedAt.Equal(expected) || t.State == models.TaskStateRunning {
		return nil, models.ErrStateConflict
	}
	if t.State == models.TaskStateCompleted && len(t.GoldenCriteria) == 0 && len(golden) > 0 {
		return nil, models.ErrStateConflict
	}
	if err := models.ValidateGoldenCriteria(criteria, golden); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrInvalidDraftPlan, err)
	}
	t.SuccessCriteria = append([]string(nil), criteria...)
	t.GoldenCriteria = append([]models.GoldenCriterion(nil), golden...)
	t.CriteriaMet = []string{}
	t.UpdatedAt = now()
	s.tasks[id] = t
	s.events = append(s.events, models.Event{BaseEntity: models.BaseEntity{CreatedAt: t.UpdatedAt}, ProjectID: t.ProjectID, TaskID: sql.NullString{String: id, Valid: true}, Type: "GOLDEN_CRITERIA_RESET"})
	return &t, nil
}
