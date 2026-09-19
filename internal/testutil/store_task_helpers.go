package testutil

import (
	"context"
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
	return created, nil
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
	s.blockParentTaskLocked(parentID, ts)
	tasks := make([]models.Task, 0, len(children))
	for _, child := range children {
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
		s.childParents[t.ID] = append(s.childParents[t.ID], parentID)
		if child.DependsOnID != "" {
			s.childParents[t.ID] = append(s.childParents[t.ID], child.DependsOnID)
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}
