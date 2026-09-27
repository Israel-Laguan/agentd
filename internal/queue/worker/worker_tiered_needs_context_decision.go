package worker

import (
	"context"
	"fmt"
	"log/slog"

	"agentd/internal/models"
)

func (w *Worker) handleFreshDecision(ctx context.Context, task models.Task, dependency models.Task, fresh *models.Task) bool {
	switch fresh.State {
	case models.TaskStateCompleted:
		return w.rewireToCompletedDecision(ctx, task, dependency, fresh.ID)
	case models.TaskStateFailed, models.TaskStateFailedRequiresHuman:
		if _, err := w.store.RewireDependsOn(ctx, dependency.ID, fresh.ID); err != nil {
			w.FailHard(ctx, task, fmt.Errorf("tiered rewire to failed decision failed: %w", err))
			return true
		}
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", fresh.ID, fresh.State))
		return true
	case models.TaskStateNeedsContext:
		if task.State.CanTransitionTo(models.TaskStateBlocked) {
			if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
				w.FailHard(ctx, task, fmt.Errorf("tiered park while awaiting fresh context failed: %w", err))
			}
		}
		return true
	default:
		if task.State.CanTransitionTo(models.TaskStateBlocked) {
			if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
				w.FailHard(ctx, task, fmt.Errorf("tiered park before dependency rewire failed: %w", err))
				return true
			}
		}
		if _, err := w.store.RewireDependsOn(ctx, dependency.ID, fresh.ID); err != nil {
			w.FailHard(ctx, task, fmt.Errorf("tiered rewire to fresh decision failed: %w", err))
			return true
		}
		return true
	}
}

func (w *Worker) handleStaleDecision(ctx context.Context, task models.Task, dependency models.Task, origin models.Task) bool {
	completedID, err := w.freshTerminalDecisionID(ctx, origin.ID, dependency.ID, models.TaskStateCompleted)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered completed decision lookup failed: %w", err))
		return true
	}
	if completedID != "" {
		return w.rewireToCompletedDecision(ctx, task, dependency, completedID)
	}
	failedID, err := w.freshTerminalDecisionID(ctx, origin.ID, dependency.ID, models.TaskStateFailed)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered failed decision lookup failed: %w", err))
		return true
	}
	if failedID != "" {
		_, _ = w.store.RewireDependsOn(ctx, dependency.ID, failedID)
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", failedID, models.TaskStateFailed))
		return true
	}
	failedHumanID, err := w.freshTerminalDecisionID(ctx, origin.ID, dependency.ID, models.TaskStateFailedRequiresHuman)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered human-failed decision lookup failed: %w", err))
		return true
	}
	if failedHumanID != "" {
		_, _ = w.store.RewireDependsOn(ctx, dependency.ID, failedHumanID)
		w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", failedHumanID, models.TaskStateFailedRequiresHuman))
		return true
	}
	if task.State.CanTransitionTo(models.TaskStateBlocked) {
		if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
			w.FailHard(ctx, task, fmt.Errorf("tiered park while awaiting context re-gather failed: %w", err))
		}
	}
	return true
}

func (w *Worker) rewireToCompletedDecision(ctx context.Context, task models.Task, dependency models.Task, decisionID string) bool {
	if _, err := w.store.RewireDependsOn(ctx, dependency.ID, decisionID); err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered rewire to completed decision failed: %w", err))
		return true
	}
	if w.allDependenciesResolved(ctx, task.ID) {
		latest, err := w.store.GetTask(ctx, task.ID)
		if err == nil && latest.State.CanTransitionTo(models.TaskStateReady) {
			if _, err := w.store.UpdateTaskState(ctx, latest.ID, latest.UpdatedAt, models.TaskStateReady); err != nil {
				slog.Error("tiered: failed to re-ready after completed fresh decision", "task_id", latest.ID, "error", err)
			}
		}
	}
	return true
}
