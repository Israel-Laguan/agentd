package queue

import (
	"context"
	"time"

	"agentd/internal/models"
)

func (s *queueStore) AddComment(_ context.Context, c models.Comment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments = append(s.comments, c)
	return nil
}

func (s *queueStore) ListComments(context.Context, string) ([]models.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Comment(nil), s.comments...), nil
}

func (s *queueStore) ListCommentsSince(_ context.Context, _ string, since time.Time) ([]models.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Comment
	for _, c := range s.comments {
		if since.IsZero() || c.UpdatedAt.After(since) {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *queueStore) ListUnprocessedHumanComments(context.Context) ([]models.CommentRef, error) {
	return nil, nil
}

func (s *queueStore) MarkCommentProcessed(context.Context, string, string) error { return nil }

func (s *queueStore) AppendEvent(_ context.Context, e models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *queueStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, nil
}

func (s *queueStore) MarkEventsCurated(context.Context, string) error   { return nil }
func (s *queueStore) DeleteCuratedEvents(context.Context, string) error { return nil }
func (s *queueStore) ListCompletedTasksOlderThan(context.Context, time.Duration) ([]models.Task, error) {
	return nil, nil
}

func (s *queueStore) RecordMemory(context.Context, models.Memory) error { return nil }
func (s *queueStore) ListMemories(context.Context, models.MemoryFilter) ([]models.Memory, error) {
	return nil, nil
}

func (s *queueStore) RecallMemories(context.Context, models.RecallQuery) ([]models.Memory, error) {
	return nil, nil
}

func (s *queueStore) TouchMemories(context.Context, []string) error             { return nil }
func (s *queueStore) SupersedeMemories(context.Context, []string, string) error { return nil }
func (s *queueStore) ListUnsupersededMemories(context.Context) ([]models.Memory, error) {
	return nil, nil
}

func (s *queueStore) GetAgentProfile(context.Context, string) (*models.AgentProfile, error) {
	return &s.profile, nil
}

func (s *queueStore) UpsertAgentProfile(context.Context, models.AgentProfile) error { return nil }

func (s *queueStore) ListAgentProfiles(context.Context) ([]models.AgentProfile, error) {
	return []models.AgentProfile{s.profile}, nil
}

func (s *queueStore) DeleteAgentProfile(context.Context, string) error { return nil }

func (s *queueStore) AssignTaskAgent(context.Context, string, time.Time, string) (*models.Task, error) {
	return nil, nil
}

func (s *queueStore) ListSettings(context.Context) ([]models.Setting, error) { return nil, nil }

func (s *queueStore) GetSetting(context.Context, string) (string, bool, error) {
	return "", false, nil
}

func (s *queueStore) SetSetting(context.Context, string, string) error { return nil }

func (s *queueStore) update(id string, apply func(*models.Task)) (*models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.tasks {
		if s.tasks[i].ID == id {
			apply(&s.tasks[i])
			s.tasks[i].UpdatedAt = time.Now().UTC()
			task := s.tasks[i]
			return &task, nil
		}
	}
	return nil, models.ErrTaskNotFound
}

func (s *queueStore) updateFirst(apply func(*models.Task)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.tasks) == 0 {
		return models.ErrTaskNotFound
	}
	apply(&s.tasks[0])
	s.tasks[0].UpdatedAt = time.Now().UTC()
	return nil
}

func (s *queueStore) count(state models.TaskState) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, task := range s.tasks {
		if task.State == state {
			count++
		}
	}
	return count
}

func (s *queueStore) first(state models.TaskState) (models.Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range s.tasks {
		if task.State == state {
			return task, true
		}
	}
	return models.Task{}, false
}
