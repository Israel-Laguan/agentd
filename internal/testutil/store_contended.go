package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"agentd/internal/models"
)

// ContendedStateStore is a FakeKanbanStore whose UpdateTaskState enforces the
// optimistic-lock version the way the real store does (transition check, then UPDATE ... WHERE
// updated_at = ?, ErrOptimisticLock when nothing matched), which the embedded
// fake deliberately ignores. Its first Contended calls first let "another
// writer" commit to the row, so the caller's version is stale by the time the
// write runs.
type ContendedStateStore struct {
	*FakeKanbanStore

	mu        sync.Mutex
	contended int
	attempts  int
}

// NewContendedStateStore returns a store whose first contended UpdateTaskState
// calls lose the optimistic lock.
func NewContendedStateStore(fake *FakeKanbanStore, contended int) *ContendedStateStore {
	return &ContendedStateStore{FakeKanbanStore: fake, contended: contended}
}

// StateUpdateAttempts reports how many UpdateTaskState calls reached the store.
func (s *ContendedStateStore) StateUpdateAttempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempts
}

func (s *ContendedStateStore) UpdateTaskState(ctx context.Context, id string, expected time.Time, next models.TaskState) (*models.Task, error) {
	s.mu.Lock()
	s.attempts++
	contend := s.contended > 0
	if contend {
		s.contended--
	}
	s.mu.Unlock()

	current, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if contend {
		// The concurrent writer: any committed write moves updated_at.
		if _, err := s.UpdateTaskDescription(ctx, id, current.UpdatedAt, current.Description); err != nil {
			return nil, err
		}
		if current, err = s.GetTask(ctx, id); err != nil {
			return nil, err
		}
	}
	// Transition first, version second: the order the real store checks in.
	if !current.State.CanTransitionTo(next) {
		return nil, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, current.State, next)
	}
	if !current.UpdatedAt.Equal(expected) {
		return nil, models.ErrOptimisticLock
	}
	return s.FakeKanbanStore.UpdateTaskState(ctx, id, expected, next)
}
