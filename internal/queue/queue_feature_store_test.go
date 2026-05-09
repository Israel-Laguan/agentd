package queue

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"agentd/internal/models"
)

type queueStore struct {
	mu       sync.Mutex
	tasks    []models.Task
	children []models.Task
	comments []models.Comment
	events   []models.Event
	project  models.Project
	profile  models.AgentProfile
}

func newQueueStore() *queueStore {
	return &queueStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "project"}, WorkspacePath: "/tmp"},
		profile: models.AgentProfile{ID: "default", Temperature: 0.2, SystemPrompt: sql.NullString{
			String: "Return JSON.", Valid: true,
		}},
	}
}

func (s *queueStore) seed(count int, state models.TaskState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = nil
	now := time.Now().UTC()
	for i := range count {
		s.tasks = append(s.tasks, models.Task{
			BaseEntity: models.BaseEntity{ID: fmt.Sprintf("task-%d", i), CreatedAt: now.Add(time.Duration(i)), UpdatedAt: now},
			ProjectID:  "project", AgentID: "default", Title: fmt.Sprintf("Task %d", i), State: state, Assignee: models.TaskAssigneeSystem,
		})
	}
}
