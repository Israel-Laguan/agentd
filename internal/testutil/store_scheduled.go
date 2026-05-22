package testutil

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) ListScheduledTasks(context.Context) ([]models.ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduled == nil {
		return nil, nil
	}
	out := make([]models.ScheduledTask, 0, len(s.scheduled))
	for _, t := range s.scheduled {
		out = append(out, t)
	}
	return out, nil
}

func (s *FakeKanbanStore) UpsertScheduledTask(_ context.Context, t models.ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduled == nil {
		s.scheduled = make(map[string]models.ScheduledTask)
	}
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now()
	}
	t.UpdatedAt = now()
	s.scheduled[t.ID] = t
	return nil
}

func (s *FakeKanbanStore) DeleteScheduledTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduled != nil {
		delete(s.scheduled, id)
	}
	return nil
}

func (s *FakeKanbanStore) UpdateScheduledTaskLastFired(_ context.Context, id string, firedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.scheduled[id]
	if !ok {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	t.LastFiredAt = &firedAt
	t.UpdatedAt = now()
	s.scheduled[id] = t
	return nil
}

func (s *FakeKanbanStore) ScheduleDeferredRequeue(_ context.Context, taskID string, runAfter time.Time) error {
	return s.UpsertScheduledTask(context.Background(), models.ScheduledTask{
		ID:           "defer:" + taskID,
		RunAfter:     &runAfter,
		Kind:         models.ScheduledTaskKindRequeue,
		TargetTaskID: taskID,
		Enabled:      true,
	})
}

func (s *FakeKanbanStore) InsertReadyTask(_ context.Context, projectID string, draft models.DraftTask) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := models.Task{
		BaseEntity:      models.BaseEntity{ID: s.nextID(), CreatedAt: now(), UpdatedAt: now()},
		ProjectID:       projectID,
		AgentID:         "default",
		Title:           draft.Title,
		Description:     draft.Description,
		Assignee:        draft.Assignee,
		State:           models.TaskStateReady,
		SuccessCriteria: append([]string(nil), draft.SuccessCriteria...),
	}
	if task.Assignee == "" {
		task.Assignee = models.TaskAssigneeSystem
	}
	if strings.TrimSpace(task.Description) == "" {
		task.Description = task.Title
	}
	s.tasks[task.ID] = task
	return &task, nil
}

// ScheduledTasks returns scheduled registry entries for assertions.
func (s *FakeKanbanStore) ScheduledTasks() []models.ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduled == nil {
		return nil
	}
	out := make([]models.ScheduledTask, 0, len(s.scheduled))
	for _, t := range s.scheduled {
		out = append(out, t)
	}
	return out
}
