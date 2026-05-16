package testutil

import (
	"context"
	"time"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	return &t, nil
}

func (s *FakeKanbanStore) ListTasksByProject(_ context.Context, projectID string) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Task
	for _, t := range s.tasks {
		if t.ProjectID == projectID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) ClaimNextReadyTasks(_ context.Context, limit int) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var claimed []models.Task
	for id, t := range s.tasks {
		if len(claimed) >= limit {
			break
		}
		if t.State == models.TaskStateReady {
			t.State = models.TaskStateQueued
			t.UpdatedAt = now()
			s.tasks[id] = t
			claimed = append(claimed, t)
		}
	}
	return claimed, nil
}

func (s *FakeKanbanStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, pid int) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	n := now()
	t.State = models.TaskStateRunning
	t.OSProcessID = &pid
	t.LastHeartbeat = &n
	t.UpdatedAt = n
	s.tasks[id] = t
	return &t, nil
}

func (s *FakeKanbanStore) UpdateTaskHeartbeat(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return models.ErrTaskNotFound
	}
	n := now()
	t.LastHeartbeat = &n
	s.tasks[id] = t
	return nil
}

func (s *FakeKanbanStore) IncrementRetryCount(_ context.Context, id string, _ time.Time) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	t.RetryCount++
	t.UpdatedAt = now()
	s.tasks[id] = t
	return &t, nil
}

func (s *FakeKanbanStore) UpdateTaskState(_ context.Context, id string, _ time.Time, next models.TaskState) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	t.State = next
	t.OSProcessID = nil
	t.UpdatedAt = now()
	s.tasks[id] = t
	return &t, nil
}

func (s *FakeKanbanStore) UpdateTaskResult(_ context.Context, id string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	if result.Success {
		t.State = models.TaskStateCompleted
	} else {
		t.State = models.TaskStateFailed
	}
	t.UpdatedAt = now()
	s.tasks[id] = t
	return &t, nil
}

func (s *FakeKanbanStore) ReconcileGhostTasks(_ context.Context, alivePIDs []int) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	alive := pidSet(alivePIDs)
	var recovered []models.Task
	for id, t := range s.tasks {
		if t.State != models.TaskStateRunning || t.OSProcessID == nil {
			continue
		}
		if _, ok := alive[*t.OSProcessID]; ok {
			continue
		}
		t.State = models.TaskStateReady
		t.OSProcessID = nil
		s.tasks[id] = t
		recovered = append(recovered, t)
	}
	return recovered, nil
}

func (s *FakeKanbanStore) ReconcileStaleTasks(_ context.Context, alivePIDs []int, stale time.Duration) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	alive := pidSet(alivePIDs)
	cutoff := now().Add(-stale)
	var recovered []models.Task
	for id, t := range s.tasks {
		if t.State != models.TaskStateRunning {
			continue
		}
		pidAlive := false
		if t.OSProcessID != nil {
			_, pidAlive = alive[*t.OSProcessID]
		}
		heartbeatStale := t.LastHeartbeat == nil || t.LastHeartbeat.Before(cutoff)
		if pidAlive && !heartbeatStale {
			continue
		}
		t.State = models.TaskStateReady
		t.OSProcessID = nil
		t.LastHeartbeat = nil
		s.tasks[id] = t
		recovered = append(recovered, t)
	}
	return recovered, nil
}

func (s *FakeKanbanStore) BlockTaskWithSubtasks(_ context.Context, id string, _ time.Time, drafts []models.DraftTask) (*models.Task, []models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, nil, models.ErrTaskNotFound
	}
	t.State = models.TaskStateBlocked
	t.OSProcessID = nil
	t.UpdatedAt = now()
	s.tasks[id] = t
	children := make([]models.Task, 0, len(drafts))
	for _, d := range drafts {
		child := models.Task{
			BaseEntity:      models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
			ProjectID:       t.ProjectID,
			AgentID:         "default",
			Title:           d.Title,
			Description:     d.Description,
			State:           models.TaskStateReady,
			Assignee:        d.Assignee,
			SuccessCriteria: append([]string(nil), d.SuccessCriteria...),
		}
		s.tasks[child.ID] = child
		children = append(children, child)
	}
	return &t, children, nil
}

func (s *FakeKanbanStore) AppendTasksToProject(_ context.Context, projectID, _ string, drafts []models.DraftTask) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var created []models.Task
	for _, d := range drafts {
		task := models.Task{
			BaseEntity:      models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
			ProjectID:       projectID,
			AgentID:         "default",
			Title:           d.Title,
			Description:     d.Description,
			State:           models.TaskStateReady,
			Assignee:        d.Assignee,
			SuccessCriteria: append([]string(nil), d.SuccessCriteria...),
		}
		s.tasks[task.ID] = task
		created = append(created, task)
	}
	return created, nil
}

func pidSet(pids []int) map[int]struct{} {
	s := make(map[int]struct{}, len(pids))
	for _, p := range pids {
		s[p] = struct{}{}
	}
	return s
}
