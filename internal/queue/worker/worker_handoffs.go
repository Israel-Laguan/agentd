package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/recovery"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

func (w *Worker) handleGatewayError(ctx context.Context, task models.Task, err error) {
	if safety.ClassifiesAsBreakerFailure(err) {
		if w.breaker != nil {
			w.breaker.RecordError(err)
			if w.breaker.IsOpen() {
				w.createProviderExhaustedHandoff(ctx, task, err)
				return
			}
		}
		w.requeue(ctx, task, fmt.Sprintf("LLM outage: %v", err))
		return
	}
	w.handleAgentFailure(ctx, task, fmt.Sprintf("gateway error: %v", err))
}

func (w *Worker) createProviderExhaustedHandoff(ctx context.Context, task models.Task, err error) {
	description := fmt.Sprintf(
		"All configured AI providers failed and the circuit breaker is open. Human review is required before this task can continue.\n\nLast gateway error:\n%s",
		truncate(err.Error(), 1500),
	)
	_, _, blockErr := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       "Manual review required: AI providers unavailable",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if blockErr != nil {
		w.emit(ctx, task, "ERROR", blockErr.Error())
		return
	}
	w.emit(ctx, task, "PROVIDER_EXHAUSTED_HANDOFF", truncate(description, 1000))
}

func (w *Worker) handlePromptRecovery(
	ctx context.Context,
	task models.Task,
	project models.Project,
	command string,
	result sandbox.Result,
) {
	detection := safety.DetectPrompt(result.Stdout, result.Stderr)
	payload := promptPayload(command, detection, result)
	w.emit(ctx, task, "PROMPT_DETECTED", truncate(payload, 1000))

	recoverable, recoveredCommand := recovery.CanRecover(command)
	if recoverable && task.RetryCount == 0 {
		retried, err := w.store.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
		if err != nil {
			w.emit(ctx, task, "ERROR", err.Error())
			return
		}
		recoveredResult, runErr := w.sandbox.Execute(ctx, w.payload(*retried, project, recoveredCommand))
		if runErr == nil && recoveredResult.Success {
			w.commit(ctx, *retried, recoveredResult, nil)
			return
		}
		payload = promptPayload(recoveredCommand, detection, recoveredResult)
		if runErr != nil {
			payload += "\nRecovery error: " + runErr.Error()
		}
		w.createPromptHandoff(ctx, *retried, payload)
		return
	}

	w.createPromptHandoff(ctx, task, payload)
}

func (w *Worker) createPromptHandoff(ctx context.Context, task models.Task, payload string) {
	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       "Manual action required: command waiting for input",
		Description: "The worker detected an interactive prompt and could not safely recover automatically.\n\n" + truncate(payload, 1500),
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "PROMPT_HANDOFF", truncate(payload, 1000))
}

func (w *Worker) handlePermissionFailure(ctx context.Context, task models.Task, command string, result sandbox.Result) {
	detection := safety.DetectPermission(result.Stdout, result.Stderr)
	payload := permissionPayload(command, detection, result)
	w.emit(ctx, task, "PERMISSION_DETECTED", truncate(payload, 1000))
	w.createPermissionHandoff(ctx, task, payload)
}

func (w *Worker) createPermissionHandoff(ctx context.Context, task models.Task, payload string) {
	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title: "Manual action required: privileged command",
		Description: "The worker detected a command that requires host privileges. " +
			"Please run the required command on the host machine with appropriate privileges and mark this task Complete.\n\n" +
			truncate(payload, 1500),
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "PERMISSION_HANDOFF", truncate(payload, 1000))
}

func (w *Worker) handleGoalStalled(ctx context.Context, task models.Task, gt *GoalTracker) error {
	goal := gt.Goal()
	if goal == nil {
		return nil
	}
	description := fmt.Sprintf(
		"The agent's goal has stalled after %d turns with %.0f%% progress.\n\nCompleted: %d/%d criteria\nBlocked: %d criteria\n\nBlocked criteria:\n%s",
		goal.TurnsActive,
		goal.ProgressRatio()*100,
		len(goal.CompletedCriteria),
		len(goal.SuccessCriteria),
		len(goal.BlockedCriteria),
		formatCriteria(goal.BlockedCriteria),
	)
	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       "Goal stalled: manual review required",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return err
	}
	w.emit(ctx, task, string(models.EventTypeGoalStalled), truncate(description, 1000))
	return nil
}

func formatCriteria(criteria []string) string {
	if len(criteria) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for _, c := range criteria {
		fmt.Fprintf(&b, "- %s\n", c)
	}
	return b.String()
}

func (w *Worker) createHealingHandoff(ctx context.Context, task models.Task, action planning.HealingAction, payload string) {
	description := fmt.Sprintf(
		"The worker exhausted automatic self-healing actions and needs human review.\n\nReason: %s\n\nLast failure:\n%s",
		action.Reason,
		truncate(payload, 1500),
	)
	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       "Manual review required: self-healing failed",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "HEALING_HANDOFF", truncate(description, 1000))
}
