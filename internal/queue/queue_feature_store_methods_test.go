package queue

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *queueStore) Close() error { return nil }

func (s *queueStore) MaterializePlan(context.Context, models.DraftPlan) (*models.Project, []models.Task, error) {
	return &s.project, nil, nil
}

func (s *queueStore) GetProject(context.Context, string) (*models.Project, error) {
	return &s.project, nil
}

func (s *queueStore) ListProjects(context.Context) ([]models.Project, error) {
	return []models.Project{s.project}, nil
}

func (s *queueStore) GetTask(_ context.Context, id string) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			task := s.tasks[i]
			return &task, nil
		}
	}
	return nil, models.ErrTaskNotFound
}

func (s *queueStore) ListTasksByProject(context.Context, string) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Task(nil), s.tasks...), nil
}

func (s *queueStore) ClaimNextReadyTasks(_ context.Context, limit int) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var claimed []models.Task
	now := time.Now().UTC()
	for i := range s.tasks {
		if len(claimed) == limit {
			break
		}
		if s.tasks[i].State == models.TaskStateReady {
			s.tasks[i].State = models.TaskStateQueued
			s.tasks[i].UpdatedAt = now
			claimed = append(claimed, s.tasks[i])
		}
	}
	return claimed, nil
}

func (s *queueStore) MarkTaskRunning(_ context.Context, id string, _ time.Time, pid int) (*models.Task, error) {
	return s.update(id, func(task *models.Task) {
		now := time.Now().UTC()
		task.State = models.TaskStateRunning
		task.OSProcessID = &pid
		task.LastHeartbeat = &now
	})
}

func (s *queueStore) UpdateTaskHeartbeat(_ context.Context, id string) error {
	_, err := s.update(id, func(task *models.Task) {
		now := time.Now().UTC()
		task.LastHeartbeat = &now
	})
	return err
}

func (s *queueStore) IncrementRetryCount(_ context.Context, id string, _ time.Time) (*models.Task, error) {
	return s.update(id, func(task *models.Task) { task.RetryCount++ })
}

func (s *queueStore) UpdateTaskState(_ context.Context, id string, _ time.Time, next models.TaskState) (*models.Task, error) {
	return s.update(id, func(task *models.Task) { task.State = next; task.OSProcessID = nil })
}

func (s *queueStore) UpdateTaskResult(_ context.Context, id string, _ time.Time, result models.TaskResult) (*models.Task, error) {
	next := models.TaskStateFailed
	if result.Success {
		next = models.TaskStateCompleted
	}
	return s.update(id, func(task *models.Task) { task.State = next })
}

func (s *queueStore) ReconcileGhostTasks(_ context.Context, alivePIDs []int) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	alive := map[int]struct{}{}
	for _, pid := range alivePIDs {
		alive[pid] = struct{}{}
	}
	var recovered []models.Task
	for i := range s.tasks {
		pid := s.tasks[i].OSProcessID
		if s.tasks[i].State != models.TaskStateRunning || pid == nil {
			continue
		}
		if _, ok := alive[*pid]; ok {
			continue
		}
		s.tasks[i].State = models.TaskStateReady
		s.tasks[i].OSProcessID = nil
		recovered = append(recovered, s.tasks[i])
	}
	return recovered, nil
}

func (s *queueStore) ReconcileOrphanedQueued(_ context.Context, minAge time.Duration) ([]models.Task, error) {
	if minAge <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().UTC().Add(-minAge)
	var recovered []models.Task
	for i := range s.tasks {
		if s.tasks[i].State != models.TaskStateQueued {
			continue
		}
		if !s.tasks[i].UpdatedAt.Before(cutoff) {
			continue
		}
		s.tasks[i].State = models.TaskStateReady
		s.tasks[i].OSProcessID = nil
		s.tasks[i].LastHeartbeat = nil
		recovered = append(recovered, s.tasks[i])
	}
	return recovered, nil
}

func (s *queueStore) ReconcileStaleTasks(_ context.Context, alivePIDs []int, staleThreshold time.Duration) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	alive := map[int]struct{}{}
	for _, pid := range alivePIDs {
		alive[pid] = struct{}{}
	}
	staleBefore := time.Now().UTC().Add(-staleThreshold)
	var recovered []models.Task
	for i := range s.tasks {
		if s.tasks[i].State != models.TaskStateRunning {
			continue
		}
		pid := s.tasks[i].OSProcessID
		_, pidAlive := alive[-1]
		if pid != nil {
			_, pidAlive = alive[*pid]
		}
		heartbeatStale := s.tasks[i].LastHeartbeat == nil || s.tasks[i].LastHeartbeat.Before(staleBefore)
		if pidAlive && !heartbeatStale {
			continue
		}
		s.tasks[i].State = models.TaskStateReady
		s.tasks[i].OSProcessID = nil
		s.tasks[i].LastHeartbeat = nil
		recovered = append(recovered, s.tasks[i])
	}
	return recovered, nil
}

func (s *queueStore) BlockTaskWithSubtasks(_ context.Context, id string, _ time.Time, drafts []models.DraftTask) (*models.Task, []models.Task, error) {
	parent, err := s.update(id, func(task *models.Task) {
		task.State = models.TaskStateBlocked
		task.OSProcessID = nil
	})
	if err != nil {
		return nil, nil, err
	}
	children := make([]models.Task, 0, len(drafts))
	for i, draft := range drafts {
		child := models.Task{
			BaseEntity:  models.BaseEntity{ID: fmt.Sprintf("blocked-child-%d", i), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()},
			ProjectID:   parent.ProjectID,
			AgentID:     "default",
			Title:       draft.Title,
			Description: draft.Description,
			State:       models.TaskStateReady,
			Assignee:    draft.Assignee,
		}
		children = append(children, child)
	}
	s.mu.Lock()
	s.children = append(s.children, children...)
	s.mu.Unlock()
	return parent, children, nil
}

func (s *queueStore) AppendTasksToProject(context.Context, string, string, []models.DraftTask) ([]models.Task, error) {
	return nil, nil
}

func (s *queueStore) EnsureSystemProject(context.Context) (*models.Project, error) {
	return &models.Project{BaseEntity: models.BaseEntity{ID: "system"}, Name: "_system"}, nil
}

func (s *queueStore) EnsureProjectTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	return &models.Task{BaseEntity: models.BaseEntity{ID: "system-task"}, ProjectID: projectID, Title: draft.Title, Description: draft.Description, Assignee: draft.Assignee}, true, nil
}
