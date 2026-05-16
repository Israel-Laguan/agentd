// Package testutil provides shared test doubles that depend only on internal/models.
package testutil

import (
	"database/sql"
	"sync"
	"time"

	"agentd/internal/models"

	"github.com/google/uuid"
)

// FakeKanbanStore is an in-memory models.KanbanStore for cross-package tests
// that need a board without importing internal/kanban.
type FakeKanbanStore struct {
	mu           sync.Mutex
	projects     map[string]models.Project
	tasks        map[string]models.Task
	childParents map[string][]string
	events       []models.Event
	comments     []models.Comment
	memories     []models.Memory
	profiles     map[string]models.AgentProfile
	settings     map[string]string
	nextSeq      int
}

var _ models.KanbanStore = (*FakeKanbanStore)(nil)

func NewFakeStore() *FakeKanbanStore {
	return &FakeKanbanStore{
		projects:     make(map[string]models.Project),
		tasks:        make(map[string]models.Task),
		childParents: make(map[string][]string),
		profiles: map[string]models.AgentProfile{
			"default": {ID: "default", Temperature: 0.2, SystemPrompt: sql.NullString{String: "Return JSON.", Valid: true}},
		},
		settings: make(map[string]string),
	}
}

func (s *FakeKanbanStore) Close() error { return nil }

// Events returns a snapshot of stored events for test assertions.
func (s *FakeKanbanStore) Events() []models.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.Event(nil), s.events...)
}

// Tasks returns a snapshot of all tasks for test assertions.
func (s *FakeKanbanStore) Tasks() []models.Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		out = append(out, t)
	}
	return out
}

func (s *FakeKanbanStore) nextID() string {
	s.nextSeq++
	return uuid.NewString()
}

func now() time.Time { return time.Now().UTC() }
