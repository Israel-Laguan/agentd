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
	origin, ok := s.tasks[originID]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	if origin.State != models.TaskStateReady && origin.State != models.TaskStateBlocked {
		return nil, fmt.Errorf("tiered continuation origin %s is %s, want BLOCKED", originID, origin.State)
	}
	seenIDs := make(map[string]struct{})
	ts := now()
	resolved := make([]models.Task, 0, len(children))
	for _, child := range children {
		t := child.Task
		if t.ID == "" {
			t.ID = s.nextID()
		}
		if _, dup := seenIDs[t.ID]; dup {
			return nil, fmt.Errorf("duplicate child ID in SpawnTieredContinuation: %s", t.ID)
		}
		seenIDs[t.ID] = struct{}{}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = ts
		}
		if t.UpdatedAt.IsZero() {
			t.UpdatedAt = ts
		}
		resolved = append(resolved, t)
	}
	for i, child := range children {
		if child.DependsOnID == "" {
			continue
		}
		if _, known := s.tasks[child.DependsOnID]; known {
			continue
		}
		found := false
		for _, sibling := range resolved[:i] {
			if sibling.ID == child.DependsOnID {
				found = true
				break
			}
		}
		if !found {
			return nil, models.ErrTaskNotFound
		}
	}
	if origin.State == models.TaskStateReady {
		origin.State = models.TaskStateBlocked
		origin.UpdatedAt = ts
		s.tasks[originID] = origin
	}
	tasks := make([]models.Task, 0, len(children))
	for i, child := range children {
		t := resolved[i]
		s.tasks[t.ID] = t
		s.childParents[t.ID] = append(s.childParents[t.ID], originID)
		s.childParentRelations[t.ID] = append(s.childParentRelations[t.ID], parentRelation{parentID: originID, relationType: models.TaskRelationSpawnedBy})
		if child.DependsOnID != "" {
			s.childParents[t.ID] = append(s.childParents[t.ID], child.DependsOnID)
			s.childParentRelations[t.ID] = append(s.childParentRelations[t.ID], parentRelation{parentID: child.DependsOnID, relationType: models.TaskRelationDependsOn})
		}
		tasks = append(tasks, t)
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
		if task.State != models.TaskStatePending && task.State != models.TaskStateReady && task.State != models.TaskStateBlocked {
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
		s.childParents[childID][idx] = newParentID
		if task.State == models.TaskStateReady {
			task.State = models.TaskStateBlocked
			task.UpdatedAt = ts
			s.tasks[childID] = task
		}
		rewired = append(rewired, task)
	}
	return rewired, nil
}
