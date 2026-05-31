package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

func (w *Worker) commit(ctx context.Context, task models.Task, result sandbox.Result, err error) {
	_ = w.commitSucceeded(ctx, task, result, err)
}

// commitSucceeded persists a successful sandbox result and reports whether the
// task result was written. Callers that must gate side effects on persistence
// (e.g. HITL consumption markers) should use this instead of commit.
func (w *Worker) commitSucceeded(ctx context.Context, task models.Task, result sandbox.Result, err error) bool {
	if safety.ClassifiesAsBreakerFailure(err) {
		w.handleGatewayError(ctx, task, err)
		return false
	}
	if err != nil || !result.Success {
		w.handleAgentFailure(ctx, task, failurePayload(result, err))
		return false
	}
	if w.breaker != nil {
		w.breaker.RecordSuccess()
	}
	_, updateErr := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: true,
		Payload: resultPayload(result),
	})
	if updateErr != nil {
		w.emit(ctx, task, "ERROR", updateErr.Error())
		return false
	}
	return true
}

func failurePayload(result sandbox.Result, err error) string {
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
}

func resultPayload(result sandbox.Result) string {
	return fmt.Sprintf("exit=%d duration=%s\n%s", result.ExitCode, result.Duration, result.Stdout)
}

func promptPayload(command string, detection safety.PromptDetection, result sandbox.Result) string {
	return fmt.Sprintf(
		"pattern=%s command=%q exit=%d duration=%s\n%s",
		detection.Pattern,
		command,
		result.ExitCode,
		result.Duration,
		truncate(strings.TrimSpace(result.Stderr+"\n"+result.Stdout), 1000),
	)
}

func permissionPayload(command string, detection safety.PermissionDetection, result sandbox.Result) string {
	return fmt.Sprintf(
		"pattern=%s command=%q exit=%d duration=%s\n%s",
		detection.Pattern,
		command,
		result.ExitCode,
		result.Duration,
		truncate(strings.TrimSpace(result.Stderr+"\n"+result.Stdout), 1000),
	)
}

func truncate(value string, max int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	suffix := "...[truncated]"
	suffixLen := len([]rune(suffix))
	if max <= suffixLen {
		return string(runes[:max])
	}
	return string(runes[:max-suffixLen]) + suffix
}

func (w *Worker) handleAgentFailure(ctx context.Context, task models.Task, payload string) {
	retried, err := w.store.IncrementRetryCount(ctx, task.ID, task.UpdatedAt)
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if w.tuner != nil {
		project, profile, err := w.loadContext(ctx, *retried)
		if err != nil {
			w.emit(ctx, *retried, "ERROR", err.Error())
			return
		}
		action := w.tuner.ForAttempt(retried.RetryCount, *profile)
		switch action.Type {
		case planning.HealingActionTune:
			w.emit(ctx, *retried, "TUNE", w.tunePayload(*profile, action, retried.RetryCount))
			w.requeue(ctx, *retried, payload)
			return
		case planning.HealingActionSplit:
			w.handleHealingSplit(ctx, *retried, *project, *profile)
			return
		case planning.HealingActionHuman:
			w.createHealingHandoff(ctx, *retried, action, payload)
			return
		}
		_ = project
	}
	if retried.RetryCount < w.maxRetries {
		w.requeue(ctx, *retried, payload)
		return
	}
	w.evict(ctx, *retried, payload)
}

func (w *Worker) handleHealingSplit(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	w.emit(ctx, task, "HEALING_SPLIT", fmt.Sprintf("attempt=%d step=%s", task.RetryCount, planning.HealingStepSplitTask))
	response, err := w.breakdownCommand(ctx, task, project, profile)
	if err != nil {
		w.createHealingHandoff(ctx, task, planning.HealingAction{
			Type:     planning.HealingActionHuman,
			StepName: planning.HealingStepHumanHandoff,
			Reason:   "failed to ask model for task breakdown: " + err.Error(),
		}, err.Error())
		return
	}
	if !response.TooComplex || len(response.Subtasks) == 0 {
		w.createHealingHandoff(ctx, task, planning.HealingAction{
			Type:     planning.HealingActionHuman,
			StepName: planning.HealingStepHumanHandoff,
			Reason:   "model could not split task after repeated failures",
		}, "worker did not return subtasks during healing split")
		return
	}
	w.handleLegacyTaskBreakdown(ctx, task, response.Subtasks, true)
}

func (w *Worker) breakdownCommand(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (workerResponse, error) {
	prompt := "This task has failed multiple times. Break it into smaller independently executable subtasks instead of attempting a single command."
	if profile.SystemPrompt.Valid {
		profile.SystemPrompt.String = profile.SystemPrompt.String + "\n\n" + prompt
	} else {
		profile.SystemPrompt.Valid = true
		profile.SystemPrompt.String = prompt
	}
	profile = w.routeLegacyProfile(ctx, task, project, profile)
	resp, tokenUsage, err := w.command(ctx, task, project, profile)
	w.recordTaskTokenUsage(ctx, task, tokenUsage)
	return resp, err
}

func (w *Worker) tunePayload(profile models.AgentProfile, action planning.HealingAction, attempt int) string {
	parts := []string{fmt.Sprintf("attempt=%d", attempt), "step=" + action.StepName}
	if action.Overrides.Temperature != nil {
		parts = append(parts, fmt.Sprintf("old_temp=%g new_temp=%g", profile.Temperature, *action.Overrides.Temperature))
	}
	if action.Overrides.MaxTokens != nil {
		parts = append(parts, fmt.Sprintf("max_tokens=%d", *action.Overrides.MaxTokens))
	}
	if action.Overrides.Model != "" {
		parts = append(parts, fmt.Sprintf("old_model=%s new_model=%s", profile.Model, action.Overrides.Model))
	}
	if action.Overrides.Provider != "" {
		parts = append(parts, fmt.Sprintf("old_provider=%s new_provider=%s", profile.Provider, action.Overrides.Provider))
	}
	if action.Overrides.Compress {
		parts = append(parts, "compress=true")
	}
	if action.Reason != "" {
		parts = append(parts, "reason="+action.Reason)
	}
	return strings.Join(parts, " ")
}

func (w *Worker) requeue(ctx context.Context, task models.Task, payload string) {
	_, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateReady)
	if err != nil && !errors.Is(err, models.ErrStateConflict) {
		w.emit(ctx, task, "ERROR", err.Error())
	}
	if strings.TrimSpace(payload) != "" {
		w.emit(ctx, task, "RETRY", truncate(payload, 1000))
	}
}

func (w *Worker) evict(ctx context.Context, task models.Task, payload string) {
	updated, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: false,
		Payload: truncate(payload, 1000),
	})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if _, err := w.store.UpdateTaskState(ctx, updated.ID, updated.UpdatedAt, models.TaskStateFailedRequiresHuman); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
	}
	w.emit(ctx, task, "POISON_PILL_HANDOFF", "Task evicted after "+fmt.Sprintf("%d", w.maxRetries)+" retries. Last error: "+truncate(payload, 500))
}

func (w *Worker) failHard(ctx context.Context, task models.Task, err error) {
	_, updateErr := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: false,
		Payload: truncate(err.Error(), 1000),
	})
	if updateErr != nil {
		w.emit(ctx, task, "ERROR", updateErr.Error())
	}
}

// failTerminal records a failed result and moves the task to a terminal state so
// it is not picked up again (used when healing handoffs are disabled or capped).
func (w *Worker) failTerminal(ctx context.Context, task models.Task, err error, state models.TaskState) {
	_, updateErr := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: false,
		Payload: truncate(err.Error(), 1000),
	})
	if updateErr != nil {
		w.emit(ctx, task, "ERROR", updateErr.Error())
		return
	}
	// UpdateTaskResult already moves RUNNING → FAILED; skip redundant transition.
	if state == models.TaskStateFailed {
		return
	}
	current, getErr := w.store.GetTask(ctx, task.ID)
	if getErr != nil {
		w.emit(ctx, task, "ERROR", getErr.Error())
		return
	}
	if _, stateErr := w.store.UpdateTaskState(ctx, current.ID, current.UpdatedAt, state); stateErr != nil {
		w.emit(ctx, task, "ERROR", stateErr.Error())
	}
}
