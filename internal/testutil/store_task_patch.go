package testutil

import (
	"context"
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
