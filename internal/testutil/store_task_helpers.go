package testutil

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func terminalCompletedAt(next models.TaskState, ts time.Time) *time.Time {
	switch next {
	case models.TaskStateCompleted, models.TaskStateFailed, models.TaskStateFailedRequiresHuman:
		return &ts
	default:
		return nil
	}
}

func pidSet(pids []int) map[int]struct{} {
	s := make(map[int]struct{}, len(pids))
	for _, p := range pids {
		s[p] = struct{}{}
	}
	return s
}

func (s *FakeKanbanStore) addDraftTasksLocked(projectID, parentTaskID string, drafts []models.DraftTask) []models.Task {
	ts := now()
	created := make([]models.Task, 0, len(drafts))
	for _, d := range drafts {
		task := s.newTaskFromDraft(projectID, ts, d)
		s.tasks[task.ID] = task
		s.childParents[task.ID] = append(s.childParents[task.ID], parentTaskID)
		created = append(created, task)
	}
	return created
}

func (s *FakeKanbanStore) blockParentTaskLocked(id string, ts time.Time) *models.Task {
	t := s.tasks[id]
	t.State = models.TaskStateBlocked
	t.OSProcessID = nil
	t.UpdatedAt = ts
	s.tasks[id] = t
	return &t
}

func (s *FakeKanbanStore) newTaskFromDraft(projectID string, ts time.Time, d models.DraftTask) models.Task {
	return models.Task{
		BaseEntity:      models.BaseEntity{ID: s.nextID(), CreatedAt: ts, UpdatedAt: ts},
		ProjectID:       projectID,
		AgentID:         resolveTaskAgentID(d.AgentID),
		Title:           d.Title,
		Description:     d.Description,
		State:           models.TaskStateReady,
		Assignee:        d.Assignee,
		SuccessCriteria: append([]string(nil), d.SuccessCriteria...),
	}
}

func (s *FakeKanbanStore) AppendTasksToProject(_ context.Context, projectID, parentTaskID string, drafts []models.DraftTask) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateDraftAgentIDs(drafts); err != nil {
		return nil, err
	}
	if _, ok := s.tasks[parentTaskID]; !ok {
		return nil, models.ErrTaskNotFound
	}
	created := s.addDraftTasksLocked(projectID, parentTaskID, drafts)
	// The real store's AppendTasksToProject inserts children as PENDING
	// (unlike BlockTaskWithSubtasks, which inserts READY subtasks).
	for i := range created {
		created[i].State = models.TaskStatePending
		s.tasks[created[i].ID] = created[i]
	}
	return created, nil
}

// resolveTieredChildrenLocked validates the plan against existing state and
// resolves IDs and timestamps without mutating any task state, so a bad
// plan fails the whole call the way the real store's transaction does.
func (s *FakeKanbanStore) resolveTieredChildrenLocked(ts time.Time, children []models.TieredDAGTask) ([]models.Task, error) {
	resolved := make([]models.Task, 0, len(children))
	persisted := make(map[string]struct{}, len(children))
	for _, child := range children {
		t := child.Task
		if t.ID == "" {
			t.ID = s.nextID()
		}
		if _, exists := s.tasks[t.ID]; exists {
			return nil, fmt.Errorf("testutil: persist tiered dag: task %q already exists: %w", t.ID, models.ErrStateConflict)
		}
		if _, dup := persisted[t.ID]; dup {
			return nil, fmt.Errorf("testutil: persist tiered dag: duplicate child id %q: %w", t.ID, models.ErrStateConflict)
		}
		// The real store inserts children in order before recording the
		// DEPENDS_ON edge, so a forward reference (or unknown task) violates
		// the task_relations foreign key and rejects the plan.
		if child.DependsOnID != "" {
			_, known := s.tasks[child.DependsOnID]
			_, earlier := persisted[child.DependsOnID]
			if !known && !earlier {
				return nil, fmt.Errorf("testutil: persist tiered dag: unknown DependsOnID %q for child %q: %w", child.DependsOnID, t.ID, models.ErrTaskNotFound)
			}
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = ts
		}
		if t.UpdatedAt.IsZero() {
			t.UpdatedAt = ts
		}
		persisted[t.ID] = struct{}{}
		resolved = append(resolved, t)
	}
	return resolved, nil
}

func (s *FakeKanbanStore) PersistTieredDAG(_ context.Context, parentID string, expectedParentUpdatedAt time.Time, children []models.TieredDAGTask) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(children) == 0 {
		return nil, models.ErrInvalidDraftPlan
	}
	parent, ok := s.tasks[parentID]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	if !parent.UpdatedAt.Equal(expectedParentUpdatedAt) {
		return nil, models.ErrStateConflict
	}
	if parent.State != models.TaskStateRunning && parent.State != models.TaskStateReady {
		return nil, models.ErrInvalidStateTransition
	}
	ts := now()
	resolved, err := s.resolveTieredChildrenLocked(ts, children)
	if err != nil {
		return nil, err
	}
	s.blockParentTaskLocked(parentID, ts)
	tasks := make([]models.Task, 0, len(resolved))
	for i, child := range children {
		t := resolved[i]
		s.tasks[t.ID] = t
		s.childParents[t.ID] = append(s.childParents[t.ID], parentID)
		s.childParentRelations[t.ID] = append(s.childParentRelations[t.ID], parentRelation{parentID: parentID, relationType: models.TaskRelationSpawnedBy})
		if child.DependsOnID != "" {
			s.childParents[t.ID] = append(s.childParents[t.ID], child.DependsOnID)
			s.childParentRelations[t.ID] = append(s.childParentRelations[t.ID], parentRelation{parentID: child.DependsOnID, relationType: models.TaskRelationDependsOn})
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
