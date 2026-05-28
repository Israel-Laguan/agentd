package testutil

import (
	"context"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) UpdateCriteriaMet(_ context.Context, id string, met []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil
	}
	if t.State != models.TaskStateRunning {
		return nil
	}
	cp := append([]string(nil), met...)
	t.CriteriaMet = cp
	t.UpdatedAt = now()
	s.tasks[id] = t
	return nil
}
