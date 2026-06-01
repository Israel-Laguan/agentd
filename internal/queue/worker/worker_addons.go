package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

// === Payloads Logic ===

func failurePayload(result sandbox.Result, err error) string {
	if err != nil {
		return err.Error()
	}
	return strings.TrimSpace(result.Stderr + "\n" + result.Stdout)
}

func resultPayload(result sandbox.Result) string {
	return fmt.Sprintf("exit=%d duration=%s\n%s", result.ExitCode, result.Duration, result.Stdout)
}

func detectionPayload(pattern, command string, result sandbox.Result) string {
	return fmt.Sprintf(
		"pattern=%s command=%q exit=%d duration=%s\n%s",
		pattern,
		command,
		result.ExitCode,
		result.Duration,
		truncate(strings.TrimSpace(result.Stderr+"\n"+result.Stdout), 1000),
	)
}

func promptPayload(command string, detection safety.PromptDetection, result sandbox.Result) string {
	return detectionPayload(detection.Pattern, command, result)
}

func permissionPayload(command string, detection safety.PermissionDetection, result sandbox.Result) string {
	return detectionPayload(detection.Pattern, command, result)
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

// === Planning Logic ===

func (w *Worker) handleTaskBreakdown(ctx context.Context, task models.Task, subtasks []workerSubtask) {
	drafts := make([]models.DraftTask, 0, len(subtasks))
	for _, subtask := range subtasks {
		drafts = append(drafts, models.DraftTask{
			Title:       subtask.Title,
			Description: subtask.Description,
			Assignee:    models.TaskAssigneeSystem,
		})
	}
	if len(drafts) == 0 {
		w.handleAgentFailure(ctx, task, "worker reported task too complex without subtasks")
		return
	}
	if _, _, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, drafts); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "TASK_BREAKDOWN", fmt.Sprintf("created %d subtasks", len(drafts)))
}

func (w *Worker) handlePhasePlanning(ctx context.Context, task models.Task, project models.Project) {
	tasks, err := w.store.ListTasksByProject(ctx, project.ID)
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	intent := planning.BuildPhaseIntent(task, project, tasks)
	plan, err := w.gateway.GeneratePlan(ctx, intent)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return
	}
	if plan == nil {
		w.emit(ctx, task, "ERROR", "gateway returned nil plan")
		return
	}
	plan.Tasks = planning.RetitlePhaseContinuationTasks(plan.Tasks, planning.NextPhaseNumber(task.Title))
	created, err := w.store.AppendTasksToProject(ctx, project.ID, task.ID, plan.Tasks)
	if err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	if _, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, models.TaskResult{
		Success: true,
		Payload: fmt.Sprintf("planned next phase with %d tasks", len(created)),
	}); err != nil {
		w.emit(ctx, task, "ERROR", err.Error())
		return
	}
	w.emit(ctx, task, "PHASE_PLANNING", fmt.Sprintf("created %d phase tasks", len(created)))
}

// === Corrections Logic ===

func (w *Worker) ingestHumanCorrections(ctx context.Context, taskID string, cm *ContextManager) {
	if cm == nil {
		return
	}
	comments, err := w.store.ListCommentsSince(ctx, taskID, cm.CommentHighWater())
	if err != nil {
		slog.Warn("failed to list task comments for corrections", "task_id", taskID, "error", err)
		return
	}
	defer cm.AdvanceCommentHighWater(comments)
	for _, c := range comments {
		source, ok := correctionSourceForCommentAuthor(c.Author)
		if !ok {
			continue
		}
		if !cm.MarkCommentCorrectionSeen(c) {
			continue
		}
		if rec := ParseCorrectionComment(c.Body, source); rec != nil {
			cm.InjectCorrection(*rec)
		}
	}
}

func correctionSourceForCommentAuthor(author models.CommentAuthor) (CorrectionSource, bool) {
	switch author {
	case models.CommentAuthorUser, models.CommentAuthorFrontdesk:
		return CorrectionSourceHuman, true
	default:
		if strings.EqualFold(string(author), string(CorrectionSourceReviewer)) {
			return CorrectionSourceReviewer, true
		}
		return "", false
	}
}
