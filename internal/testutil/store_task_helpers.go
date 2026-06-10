package testutil

import (
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
