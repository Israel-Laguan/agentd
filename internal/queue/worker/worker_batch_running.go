package worker

import (
	"context"
	"fmt"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

// processRunningTask executes a task already marked RUNNING with an active heartbeat.
// Caller must not call MarkTaskRunning or start a second heartbeat.
func (w *Worker) processRunningTask(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
) {
	defer w.recoverPanic(ctx, task)
	// Guard: if this provider's circuit breaker is open, create an immediate
	// handoff rather than wasting the slot on a call that will fail with 429.
	if w.providerBreakers != nil && profile.Provider != "" {
		if w.providerBreakers.Get(profile.Provider).IsOpen() {
			w.createProviderExhaustedHandoff(ctx, task,
				fmt.Errorf("%w: provider %s circuit breaker is open",
					models.ErrLLMQuotaExceeded, profile.Provider))
			return
		}
	}
	if profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.failHard(ctx, task, err)
			return
		} else if done {
			return
		}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, project)
		return
	}
	if profile.AgenticMode {
		if result, ok := w.processAgentic(ctx, task, project, profile); ok {
			w.handleLoopResult(ctx, task, result)
		}
		return
	}
	w.runLegacyTask(ctx, task, project, profile, false)
}
