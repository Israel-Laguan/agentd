package testutil

import (
	"context"
	"time"

	"agentd/internal/models"
)

// hitlChildFailsOnTimeout reports whether reconcile should transition a child to FAILED.
// Mirrors internal/kanban failHITLBlockedTask: skip COMPLETED, FAILED, and FAILED_REQUIRES_HUMAN.
func hitlChildFailsOnTimeout(state models.TaskState) bool {
	return state != models.TaskStateCompleted &&
		state != models.TaskStateFailed &&
		state != models.TaskStateFailedRequiresHuman
}

func (s *FakeKanbanStore) ListChildTasks(_ context.Context, parentID string) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.childParents[parentID]
	out := make([]models.Task, 0, len(ids))
	for _, id := range ids {
		if t, ok := s.tasks[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) ReconcileExpiredBlockedTasks(_ context.Context, now time.Time) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expired []models.Task
	for id, t := range s.tasks {
		if t.State != models.TaskStateBlocked {
			continue
		}
		hasOpenChild := false
		for _, childID := range s.childParents[id] {
			child, ok := s.tasks[childID]
			if !ok {
				continue
			}
			if child.State != models.TaskStateCompleted && child.State != models.TaskStateFailed {
				hasOpenChild = true
				break
			}
		}
		if !hasOpenChild {
			continue
		}
		deadline, ok := s.hitlExpiryLocked(id)
		if !ok {
			continue
		}
		if !now.After(deadline) {
			continue
		}
		t.State = models.TaskStateFailedRequiresHuman
		t.UpdatedAt = now.UTC()
		s.tasks[id] = t
		expired = append(expired, t)
		for _, childID := range s.childParents[id] {
			child, ok := s.tasks[childID]
			if !ok || !hitlChildFailsOnTimeout(child.State) {
				continue
			}
			child.State = models.TaskStateFailed
			child.UpdatedAt = now.UTC()
			s.tasks[childID] = child
		}
	}
	return expired, nil
}

func (s *FakeKanbanStore) hitlExpiryLocked(taskID string) (time.Time, bool) {
	var latest time.Time
	var found bool
	for _, c := range s.comments {
		if c.TaskID != taskID {
			continue
		}
		prefix := models.HITLExpiresAtCommentPrefix
		if len(c.Body) < len(prefix) || c.Body[:len(prefix)] != prefix {
			continue
		}
		t, err := time.Parse(time.RFC3339, c.Body[len(prefix):])
		if err != nil {
			continue
		}
		if !found || t.After(latest) {
			latest = t
			found = true
		}
	}
	return latest, found
}
