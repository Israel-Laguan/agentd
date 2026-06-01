package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentruntime "agentd/internal/agent/runtime"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

type LoopStatus = agentruntime.LoopStatus
type LoopMeta = agentruntime.LoopMeta
type LoopResult = agentruntime.LoopResult

const (
	LoopSuccessfulCompletion = agentruntime.LoopSuccessfulCompletion
	LoopBudgetExhausted      = agentruntime.LoopBudgetExhausted
	LoopTurnLimitExceeded    = agentruntime.LoopTurnLimitExceeded
	LoopToolFailure          = agentruntime.LoopToolFailure
)

func (w *Worker) recordLoopResult(result LoopResult) {
	if w.loopResultRecorder != nil {
		w.loopResultRecorder(result)
	}
}

// handleLoopResult maps a typed loop outcome to existing store side effects.
// Full recovery strategies (summarize-and-continue, decomposition) are Tasks 23/27/28.
func (w *Worker) handleLoopResult(ctx context.Context, task models.Task, result LoopResult) {
	switch result.Status {
	case LoopSuccessfulCompletion:
		// commitTextWithProfile already ran inside the loop.
	case LoopBudgetExhausted:
		payload := result.Meta.LastError
		if payload == "" {
			payload = "context or token budget exhausted"
		}
		w.emit(ctx, task, "LOOP_BUDGET_EXHAUSTED", payload)
		w.handleAgentFailure(ctx, task, payload)
	case LoopTurnLimitExceeded:
		w.handleIterationExceeded(ctx, task)
	case LoopToolFailure:
		payload := result.Meta.LastError
		if result.Meta.ToolName != "" {
			payload = fmt.Sprintf("tool %s failed: %s", result.Meta.ToolName, payload)
		}
		if payload == "" {
			payload = "required tool failed repeatedly"
		}
		w.handleAgentFailure(ctx, task, payload)
	}
}

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
