package testutil

import (
	"context"

	"agentd/internal/models"
)

func (s *FakeKanbanStore) CountTasksByState(_ context.Context, includeHealing bool) ([]models.TaskStateCount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	counts := map[[2]string]int{}
	for _, t := range s.tasks {
		if !includeHealing && models.IsSelfHealingHandoffTask(t) {
			continue
		}
		counts[[2]string{t.ProjectID, string(t.State)}]++
	}
	out := make([]models.TaskStateCount, 0, len(counts))
	for key, n := range counts {
		out = append(out, models.TaskStateCount{ProjectID: key[0], State: models.TaskState(key[1]), Count: n})
	}
	return out, nil
}

func (s *FakeKanbanStore) ListAttentionTasks(_ context.Context) ([]models.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.Task
	for _, t := range s.tasks {
		switch {
		case t.State == models.TaskStateCompleted || t.State == models.TaskStateFailed:
		case t.Assignee == models.TaskAssigneeHuman, t.State == models.TaskStateBlocked,
			t.State == models.TaskStateFailedRequiresHuman, t.State == models.TaskStateInConsideration,
			t.State == models.TaskStateNeedsContext:
			out = append(out, t)
		}
	}
	return out, nil
}
