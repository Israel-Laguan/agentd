package worker

import (
	"context"
	"sync"
	"time"

	"agentd/internal/models"
)

// legacyHandoffStore extends routingTestStore to capture BlockTaskWithSubtasks calls.
type legacyHandoffStore struct {
	routingTestStore
	mu       sync.Mutex
	blocked  bool
	subtasks []models.DraftTask
}

func (s *legacyHandoffStore) ClearProjectStartedEmpty(context.Context, string) error { return nil }

func (s *legacyHandoffStore) BlockTaskWithSubtasks(_ context.Context, _ string, _ time.Time, drafts []models.DraftTask) (*models.Task, []models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocked = true
	s.subtasks = append(s.subtasks, drafts...)
	s.task.State = models.TaskStateBlocked
	return &s.task, nil, nil
}

func (s *legacyHandoffStore) PersistTieredDAG(context.Context, string, time.Time, []models.TieredDAGTask) ([]models.Task, error) {
	return nil, nil
}

func (s *legacyHandoffStore) SpawnTieredContinuation(context.Context, string, []models.TieredContinuationTask) ([]models.Task, error) {
	return nil, nil
}

func (s *legacyHandoffStore) RewireDependsOn(context.Context, string, string) ([]models.Task, error) {
	return nil, nil
}

// legacyHandoffSink records emitted event kinds.
type legacyHandoffSink struct {
	mu    sync.Mutex
	kinds []string
}

func (s *legacyHandoffSink) Emit(_ context.Context, ev models.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.kinds = append(s.kinds, string(ev.Type))
	return nil
}

func containsEvent(kinds []string, want string) bool {
	for _, k := range kinds {
		if k == want {
			return true
		}
	}
	return false
}
