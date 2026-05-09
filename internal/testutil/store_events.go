package testutil

import (
	"context"
	"database/sql"
	"time"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) AddComment(_ context.Context, c models.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments = append(s.comments, c)
	return nil
}

func (s *FakeKanbanStore) ListComments(_ context.Context, taskID string) ([]models.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Comment
	for _, c := range s.comments {
		if c.TaskID == taskID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (s *FakeKanbanStore) MarkCommentProcessed(context.Context, string, string) error { return nil }

func (s *FakeKanbanStore) AppendEvent(_ context.Context, e models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.CreatedAt = now()
	s.events = append(s.events, e)
	return nil
}

func (s *FakeKanbanStore) ListEventsByTask(_ context.Context, taskID string) ([]models.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Event
	for _, e := range s.events {
		if e.TaskID.Valid && e.TaskID.String == taskID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *FakeKanbanStore) DeleteCuratedEvents(context.Context, string) error { return nil }

func (s *FakeKanbanStore) ListCompletedTasksOlderThan(_ context.Context, age time.Duration) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := now().Add(-age)
	var out []models.Task
	for _, t := range s.tasks {
		if t.State == models.TaskStateCompleted && t.UpdatedAt.Before(cutoff) {
			out = append(out, t)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) RecordMemory(_ context.Context, m models.Memory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memories = append(s.memories, m)
	return nil
}

func (s *FakeKanbanStore) ListMemories(_ context.Context, _ models.MemoryFilter) ([]models.Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Memory(nil), s.memories...), nil
}

func (s *FakeKanbanStore) RecallMemories(_ context.Context, _ models.RecallQuery) ([]models.Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Memory
	for _, m := range s.memories {
		if !m.SupersededBy.Valid {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) TouchMemories(_ context.Context, _ []string) error { return nil }

func (s *FakeKanbanStore) SupersedeMemories(_ context.Context, oldIDs []string, newID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	oldSet := make(map[string]struct{}, len(oldIDs))
	for _, id := range oldIDs {
		oldSet[id] = struct{}{}
	}
	for i := range s.memories {
		if _, ok := oldSet[s.memories[i].ID]; ok {
			s.memories[i].SupersededBy = sql.NullString{String: newID, Valid: true}
		}
	}
	return nil
}

func (s *FakeKanbanStore) ListUnsupersededMemories(_ context.Context) ([]models.Memory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Memory
	for _, m := range s.memories {
		if !m.SupersededBy.Valid {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *FakeKanbanStore) GetAgentProfile(_ context.Context, id string) (*models.AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.profiles[id]
	if !ok {
		return nil, models.ErrAgentProfileNotFound
	}
	return &p, nil
}

func (s *FakeKanbanStore) ListAgentProfiles(_ context.Context) ([]models.AgentProfile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.AgentProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	return out, nil
}

func (s *FakeKanbanStore) UpsertAgentProfile(_ context.Context, p models.AgentProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[p.ID] = p
	return nil
}

func (s *FakeKanbanStore) DeleteAgentProfile(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "default" {
		return models.ErrAgentProfileProtected
	}
	if _, ok := s.profiles[id]; !ok {
		return models.ErrAgentProfileNotFound
	}
	for _, t := range s.tasks {
		if t.AgentID == id {
			return models.ErrAgentProfileInUse
		}
	}
	delete(s.profiles, id)
	return nil
}

func (s *FakeKanbanStore) AssignTaskAgent(
	_ context.Context, taskID string, expectedUpdatedAt time.Time, agentID string,
) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.profiles[agentID]; !ok {
		return nil, models.ErrAgentProfileNotFound
	}
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, models.ErrTaskNotFound
	}
	if !task.UpdatedAt.Equal(expectedUpdatedAt) {
		return nil, models.ErrStateConflict
	}
	if task.State == models.TaskStateRunning {
		return nil, models.ErrStateConflict
	}
	task.AgentID = agentID
	task.UpdatedAt = now()
	s.tasks[taskID] = task
	return &task, nil
}

func (s *FakeKanbanStore) ListSettings(context.Context) ([]models.Setting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Setting
	for k, v := range s.settings {
		out = append(out, models.Setting{Key: k, Value: v})
	}
	return out, nil
}

func (s *FakeKanbanStore) GetSetting(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.settings[key]
	return v, ok, nil
}

func (s *FakeKanbanStore) SetSetting(_ context.Context, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = value
	return nil
}
