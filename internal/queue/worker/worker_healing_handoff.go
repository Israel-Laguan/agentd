package worker

import (
	"context"
	"errors"
	"fmt"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

// healingTaskCapReached returns true when maxHealingTasks > 0 and the task
// already has at least that many HUMAN child tasks.
func (w *Worker) healingTaskCapReached(ctx context.Context, task models.Task) bool {
	if w.maxHealingTasks <= 0 {
		return false
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		return false
	}
	count := 0
	for _, child := range children {
		if child.Assignee == models.TaskAssigneeHuman {
			count++
		}
	}
	return count >= w.maxHealingTasks
}

func (w *Worker) createProviderExhaustedHandoff(ctx context.Context, task models.Task, err error) {
	if w.healingTaskCapReached(ctx, task) {
		w.failTerminal(ctx, task, fmt.Errorf("max_healing_tasks cap reached: %w", err), models.TaskStateFailed)
		w.emit(ctx, task, "PROVIDER_EXHAUSTED_HANDOFF", "max_healing_tasks cap reached; task failed: "+truncate(err.Error(), 500))
		return
	}
	var cause string
	if errors.Is(err, models.ErrLLMQuotaExceeded) {
		cause = "Provider quota exhausted; retry after quota reset or switch provider."
	} else {
		cause = "All configured AI providers failed and the circuit breaker is open. Human review is required before this task can continue."
	}
	description := fmt.Sprintf("%s\n\nLast gateway error:\n%s", cause, truncate(err.Error(), 1500))
	_, _, blockErr := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleManualReview + " AI providers unavailable",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if blockErr != nil {
		w.emit(ctx, task, "ERROR", blockErr.Error())
		return
	}
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
	w.emit(ctx, task, "PROVIDER_EXHAUSTED_HANDOFF", truncate(description, 1000))
}

func (w *Worker) createHealingHandoff(ctx context.Context, task models.Task, action planning.HealingAction, payload string) {
	if w.healingTaskCapReached(ctx, task) {
		w.failTerminal(ctx, task, fmt.Errorf("max_healing_tasks cap reached: %s", action.Reason), models.TaskStateFailed)
		w.emit(ctx, task, "HEALING_HANDOFF", "max_healing_tasks cap reached; task failed: "+truncate(payload, 500))
		return
	}
	description := fmt.Sprintf(
		"The worker exhausted automatic self-healing actions and needs human review.\n\nReason: %s\n\nLast failure:\n%s",
		action.Reason,
		truncate(payload, 1500),
	)
	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleManualReview + " self-healing failed",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
	w.emit(ctx, task, "HEALING_HANDOFF", truncate(description, 1000))
}
