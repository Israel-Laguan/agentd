package testutil

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

func cloneScheduledTask(t models.ScheduledTask) models.ScheduledTask {
	out := t
	if t.ContextArgs != nil {
		out.ContextArgs = make(map[string]string, len(t.ContextArgs))
		for k, v := range t.ContextArgs {
			out.ContextArgs[k] = v
		}
	}
	if t.RunAfter != nil {
		runAfter := *t.RunAfter
		out.RunAfter = &runAfter
	}
	if t.LastFiredAt != nil {
		lastFired := *t.LastFiredAt
		out.LastFiredAt = &lastFired
	}
	return out
}

func (s *FakeKanbanStore) ListScheduledTasks(context.Context) ([]models.ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.scheduled) == 0 {
		return nil, nil
	}
	out := make([]models.ScheduledTask, 0, len(s.scheduled))
	for _, t := range s.scheduled {
		out = append(out, cloneScheduledTask(t))
	}
	return out, nil
}

func (s *FakeKanbanStore) UpsertScheduledTask(_ context.Context, t models.ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t = cloneScheduledTask(t)
	if existing, ok := s.scheduled[t.ID]; ok {
		if t.LastFiredAt == nil {
			t.LastFiredAt = existing.LastFiredAt
		}
		if t.CreatedAt.IsZero() {
			t.CreatedAt = existing.CreatedAt
		}
	} else if t.CreatedAt.IsZero() {
		t.CreatedAt = now()
	}
	t.UpdatedAt = now()
	s.scheduled[t.ID] = t
	return nil
}

func (s *FakeKanbanStore) DeleteScheduledTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.scheduled, id)
	return nil
}

func (s *FakeKanbanStore) UpdateScheduledTaskLastFired(_ context.Context, id string, firedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.scheduled[id]
	if !ok {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	t = cloneScheduledTask(t)
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
	return s.insertReadyTask(projectID, draft, "", time.Time{}, false)
}

func (s *FakeKanbanStore) InsertReadyTaskAndRecordDispatch(
	_ context.Context,
	projectID string,
	draft models.DraftTask,
	scheduleID string,
	slot time.Time,
	deleteEntry bool,
) (*models.Task, error) {
	return s.insertReadyTask(projectID, draft, scheduleID, slot, deleteEntry)
}

func (s *FakeKanbanStore) insertReadyTask(
	projectID string,
	draft models.DraftTask,
	scheduleID string,
	slot time.Time,
	deleteEntry bool,
) (*models.Task, error) {
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
	if scheduleID != "" {
		entry, ok := s.scheduled[scheduleID]
		if !ok {
			return nil, fmt.Errorf("scheduled task %q not found", scheduleID)
		}
		entry = cloneScheduledTask(entry)
		entry.LastFiredAt = &slot
		entry.UpdatedAt = now()
		if deleteEntry {
			delete(s.scheduled, scheduleID)
		} else {
			s.scheduled[scheduleID] = entry
		}
	}
	s.tasks[task.ID] = task
	return &task, nil
}

// ScheduledTasks returns scheduled registry entries for assertions.
func (s *FakeKanbanStore) ScheduledTasks() []models.ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.scheduled) == 0 {
		return nil
	}
	out := make([]models.ScheduledTask, 0, len(s.scheduled))
	for _, t := range s.scheduled {
		out = append(out, cloneScheduledTask(t))
	}
	return out
}
