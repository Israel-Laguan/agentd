package testutil

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) SpawnTieredContinuation(_ context.Context, originID string, children []models.TieredContinuationTask) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(children) == 0 {
		return nil, models.ErrInvalidDraftPlan
	}
	if _, ok := s.tasks[originID]; !ok {
		return nil, models.ErrTaskNotFound
	}
	seenIDs := make(map[string]struct{})
	ts := now()
	tasks := make([]models.Task, 0, len(children))
	for _, child := range children {
		if _, dup := seenIDs[child.Task.ID]; dup {
			return nil, fmt.Errorf("duplicate child ID in SpawnTieredContinuation: %s", child.Task.ID)
		}
		seenIDs[child.Task.ID] = struct{}{}
		t := child.Task
		if t.ID == "" {
			t.ID = s.nextID()
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = ts
		}
		if t.UpdatedAt.IsZero() {
			t.UpdatedAt = ts
		}
		s.tasks[t.ID] = t
		s.childParents[t.ID] = append(s.childParents[t.ID], originID)
		s.childParentRelations[t.ID] = append(s.childParentRelations[t.ID], parentRelation{parentID: originID, relationType: models.TaskRelationSpawnedBy})
		if child.DependsOnID != "" {
			s.childParents[t.ID] = append(s.childParents[t.ID], child.DependsOnID)
			s.childParentRelations[t.ID] = append(s.childParentRelations[t.ID], parentRelation{parentID: child.DependsOnID, relationType: models.TaskRelationDependsOn})
		}
		tasks = append(tasks, t)
	}
	for _, child := range children {
		if child.DependsOnID != "" {
			if _, ok := s.tasks[child.DependsOnID]; !ok {
				return nil, models.ErrTaskNotFound
			}
		}
	}
	return tasks, nil
}

func (s *FakeKanbanStore) RewireDependsOn(_ context.Context, oldParentID, newParentID string) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[newParentID]; !ok {
		return nil, models.ErrTaskNotFound
	}
	ts := now()
	var rewired []models.Task
	for childID, rels := range s.childParentRelations {
		task, ok := s.tasks[childID]
		if !ok {
			continue
		}
		if task.State != models.TaskStatePending && task.State != models.TaskStateReady {
			continue
		}
		idx := -1
		for i, rel := range rels {
			if rel.parentID == oldParentID && rel.relationType == models.TaskRelationDependsOn {
				idx = i
				break
			}
		}
		if idx == -1 {
			continue
		}
		rels[idx] = parentRelation{parentID: newParentID, relationType: models.TaskRelationDependsOn}
		s.childParentRelations[childID] = rels
		for i, pid := range s.childParents[childID] {
			if pid == oldParentID {
				s.childParents[childID][i] = newParentID
				break
			}
		}
		if task.State == models.TaskStateReady {
			task.State = models.TaskStateBlocked
			task.UpdatedAt = ts
			s.tasks[childID] = task
		}
		rewired = append(rewired, task)
	}
	return rewired, nil
}
