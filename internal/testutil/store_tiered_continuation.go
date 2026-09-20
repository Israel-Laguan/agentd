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
	if err := validateTieredOriginState(originID, origin); err != nil {
		return nil, err
	}
	resolved, err := resolveTieredChildren(s, children)
	if err != nil {
		return nil, err
	}
	if err := validateTieredDependencies(s, children, resolved); err != nil {
		return nil, err
	}
	if origin.State == models.TaskStateReady {
		origin.State = models.TaskStateBlocked
		origin.UpdatedAt = now()
		s.tasks[originID] = origin
	}
	return persistTieredChildren(s, originID, children, resolved), nil
}

func validateTieredOriginState(originID string, origin models.Task) error {
	if origin.State != models.TaskStateReady && origin.State != models.TaskStateBlocked {
		return fmt.Errorf("tiered continuation origin %s is %s, want BLOCKED", originID, origin.State)
	}
	return nil
}

func resolveTieredChildren(s *FakeKanbanStore, children []models.TieredContinuationTask) ([]models.Task, error) {
	seenIDs := make(map[string]struct{})
	ts := now()
	resolved := make([]models.Task, 0, len(children))
	for _, child := range children {
		t := child.Task
		if t.ID == "" {
			t.ID = s.nextID()
		}
		if _, dup := seenIDs[t.ID]; dup {
			return nil, fmt.Errorf("duplicate child ID in tiered continuation: %s", t.ID)
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
	return resolved, nil
}

func persistTieredChildren(s *FakeKanbanStore, originID string, children []models.TieredContinuationTask, resolved []models.Task) []models.Task {
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
	return tasks
}

func validateTieredDependencies(s *FakeKanbanStore, children []models.TieredContinuationTask, resolved []models.Task) error {
	for i, child := range children {
		if child.DependsOnID == "" {
			continue
		}
		if child.Task.State == models.TaskStateReady {
			return fmt.Errorf("dependent continuation child %s cannot be READY", child.Task.ID)
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
			return models.ErrTaskNotFound
		}
	}
	return nil
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
