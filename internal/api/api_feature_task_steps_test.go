package api_test

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

func (s *apiScenario) runningTask(_ context.Context, id string) error {
	if id != s.store.task.ID {
		return fmt.Errorf("unexpected task id %s", id)
	}
	return nil
}

func (s *apiScenario) taskInConsideration(_ context.Context, id string) error {
	task, err := s.store.GetTask(context.Background(), id)
	if err != nil {
		return err
	}
	return requireEqual(string(task.State), string(models.TaskStateInConsideration))
}

func (s *apiScenario) taskNotTerminal(context.Context) error {
	if s.store.task.State == models.TaskStateCompleted || s.store.task.State == models.TaskStateFailed {
		return fmt.Errorf("task transitioned to %s", s.store.task.State)
	}
	return nil
}
