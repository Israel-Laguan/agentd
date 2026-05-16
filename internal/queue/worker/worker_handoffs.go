package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
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

func (w *Worker) recordLegacyHandoffExpiry(ctx context.Context, task models.Task) bool {
	if err := recordHITLExpiry(ctx, w.store, task.ID, time.Now().Add(LegacyHandoffTimeout)); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return false
	}
	return true
}

func (w *Worker) createProviderExhaustedHandoff(ctx context.Context, task models.Task, err error) {
	description := fmt.Sprintf(
		"All configured AI providers failed and the circuit breaker is open. Human review is required before this task can continue.\n\nLast gateway error:\n%s",
		truncate(err.Error(), 1500),
	)
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
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
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
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
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
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

func (w *Worker) createHealingHandoff(ctx context.Context, task models.Task, action planning.HealingAction, payload string) {
	description := fmt.Sprintf(
		"The worker exhausted automatic self-healing actions and needs human review.\n\nReason: %s\n\nLast failure:\n%s",
		action.Reason,
		truncate(payload, 1500),
	)
	if !w.recordLegacyHandoffExpiry(ctx, task) {
		return
	}
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

// createReviewHandoff blocks the task with a HUMAN subtask so a human
// can review the agent's draft output before the task is marked
// complete. Review feedback re-enters the loop as task-level context.
func (w *Worker) createReviewHandoff(ctx context.Context, task models.Task, draftOutput string) {
	if err := persistDraftReviewComment(ctx, w.store, task.ID, draftOutput); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	expiresAt := time.Now().Add(DefaultApprovalTimeout)
	if err := recordHITLExpiry(ctx, w.store, task.ID, expiresAt); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	description := FormatForHuman(HITLMessage{
		Summary: "Review required before task completion",
		Action:  "Review the draft output below. Mark this subtask COMPLETED to approve, or add a comment with feedback and mark FAILED to request changes.",
		Urgency: "blocking",
		Detail:  truncate(draftOutput, 2000),
	})

	_, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       "Review required: draft output pending approval",
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "REVIEW_HANDOFF", truncate(draftOutput, 1000))
}
