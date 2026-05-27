package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/recovery"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

// HITLMessage is the structured envelope for all human-in-the-loop
// messages. Humans who trust the agent can act on the header alone
// (Summary + Action + Urgency); the Detail section provides
// supporting context for those who need it.
type HITLMessage struct {
	Summary string
	Action  string
	Urgency string
	Detail  string
}

// FormatForHuman renders a HITLMessage into a scannable text block.
// The header (summary, required action, urgency) is separated from
// the detail section so humans can decide quickly.
func FormatForHuman(msg HITLMessage) string {
	var b strings.Builder
	b.WriteString("## Summary\n")
	b.WriteString(msg.Summary)
	b.WriteString("\n\n## Required Action\n")
	b.WriteString(msg.Action)
	b.WriteString("\n\n## Urgency\n")
	b.WriteString(msg.Urgency)
	if msg.Detail != "" {
		b.WriteString("\n\n---\n\n## Detail\n")
		b.WriteString(msg.Detail)
	}
	return b.String()
}

func (w *Worker) handleGatewayError(ctx context.Context, task models.Task, err error) {
	if safety.ClassifiesAsBreakerFailure(err) {
		if errors.Is(err, models.ErrLLMQuotaExceeded) {
			if w.providerBreakers != nil {
				w.providerBreakers.Get(w.lookupProvider(ctx, task)).RecordError(err)
			}
			w.handoffOrFail(ctx, task, err)
			return
		}
		if w.breaker != nil {
			w.breaker.RecordError(err)
			if w.breaker.IsOpen() {
				w.handoffOrFail(ctx, task, err)
				return
			}
		}
		w.requeue(ctx, task, fmt.Sprintf("LLM outage: %v", err))
		return
	}
	w.handleAgentFailure(ctx, task, fmt.Sprintf("gateway error: %v", err))
}

func (w *Worker) handoffOrFail(ctx context.Context, task models.Task, err error) {
	if !w.healingEnabled {
		w.failTerminal(ctx, task, err, models.TaskStateFailed)
		w.emit(ctx, task, "PROVIDER_EXHAUSTED_HANDOFF", "healing disabled; task failed: "+truncate(err.Error(), 500))
		return
	}
	w.createProviderExhaustedHandoff(ctx, task, err)
}

// lookupProvider returns the provider name configured for the task's agent.
// Returns an empty string on any error so callers can use it as a key prefix.
func (w *Worker) lookupProvider(ctx context.Context, task models.Task) string {
	profile, err := w.store.GetAgentProfile(ctx, task.AgentID)
	if err != nil || profile == nil {
		return ""
	}
	return profile.Provider
}

func (w *Worker) recordLegacyHandoffExpiry(ctx context.Context, task models.Task) bool {
	if err := recordHITLExpiry(ctx, w.store, task.ID, time.Now().Add(w.legacyHandoffTimeout)); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return false
	}
	return true
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
		Title:       models.HITLSubtaskTitleManualAction + " command waiting for input",
		Description: "The worker detected an interactive prompt and could not safely recover automatically.\n\n" + truncate(payload, 1500),
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if !w.recordLegacyHandoffExpiry(ctx, task) {
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
		Title: models.HITLSubtaskTitleManualAction + " privileged command",
		Description: "The worker detected a command that requires host privileges. " +
			"Please run the required command on the host machine with appropriate privileges and mark this task Complete.\n\n" +
			truncate(payload, 1500),
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if !w.recordLegacyHandoffExpiry(ctx, task) {
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
	if fresh, err := w.store.GetTask(ctx, task.ID); err != nil {
		slog.Warn("failed to refresh task version for goal stall handoff", "task_id", task.ID, "error", err)
	} else if fresh != nil {
		task = *fresh
	}
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

// createLegacyModeHandoff blocks the task with a HUMAN subtask when the
// legacy JSON-command worker cannot obtain a valid JSON response after all
// internal retries are exhausted. This typically means the task requires
// multi-step reasoning or produces too much output for a single shell
// command argument, and the agent should be switched to agentic mode.
func (w *Worker) createLegacyModeHandoff(ctx context.Context, task models.Task, cause string, err error) {
	detail := cause
	if err != nil {
		detail += "\n\nLast error:\n" + truncate(err.Error(), 1500)
	}
	description := FormatForHuman(HITLMessage{
		Summary: "Task cannot be completed in legacy (one-shot JSON) mode.",
		Action: "Switch the agent to agentic mode:\n" +
			"  PATCH /api/v1/agents/" + task.AgentID + "\n" +
			`  {"agentic_mode":true,"provider":"openai"}  ` + "(or \"anthropic\")\n" +
			"Then re-queue the task by setting its state to READY.",
		Urgency: "blocking",
		Detail:  detail,
	})
	_, _, blockErr := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleManualReview + " switch to agentic mode",
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
	w.emit(ctx, task, "LEGACY_MODE_HANDOFF", truncate(description, 1000))
}

// createReviewHandoff blocks the task with a HUMAN subtask so a human
// can review the agent's draft output before the task is marked
// complete. Review feedback re-enters the loop as task-level context.
func (w *Worker) createReviewHandoff(ctx context.Context, task models.Task, draftOutput string) {
	if fresh, err := w.store.GetTask(ctx, task.ID); err != nil {
		slog.Warn("failed to refresh task version for review handoff", "task_id", task.ID, "error", err)
	} else if fresh != nil {
		task = *fresh
	}
	if err := persistDraftReviewComment(ctx, w.store, task.ID, draftOutput); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if strings.TrimSpace(draftOutput) != "" {
		markPendingReviewRejectionConsumed(ctx, w.store, task.ID)
	}
	description := FormatForHuman(HITLMessage{
		Summary: "Review required before task completion",
		Action:  "Review the draft output below. Mark this subtask COMPLETED to approve, or add a comment with feedback and mark FAILED to request changes.",
		Urgency: "blocking",
		Detail:  truncate(draftOutput, 2000),
	})

	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleReview + " draft output pending approval",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if err := recordHITLExpiry(ctx, w.store, task.ID, time.Now().Add(DefaultApprovalTimeout)); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "REVIEW_HANDOFF", truncate(draftOutput, 1000))
}
