package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"agentd/internal/models"
)

func (w *Worker) buildElicitationFileContext(task models.Task, project models.Project) string {
	var parts []string
	if ctx := strings.TrimSpace(project.OriginalInput); ctx != "" {
		parts = append(parts, truncateRunes(ctx, 2000))
	}
	return strings.Join(parts, "\n")
}

// reblockTaskForPendingHITL moves a RUNNING parent back to BLOCKED when an open
// clarification subtask already exists (re-process after stale recovery, etc.).
func (w *Worker) reblockTaskForPendingHITL(ctx context.Context, task models.Task) (models.Task, error) {
	updated, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked)
	if err == nil {
		return *updated, nil
	}
	if errors.Is(err, models.ErrStateConflict) || errors.Is(err, models.ErrOptimisticLock) {
		fresh, getErr := w.store.GetTask(ctx, task.ID)
		if getErr != nil {
			return task, fmt.Errorf("refresh task for re-block: %w", getErr)
		}
		if fresh == nil {
			return task, fmt.Errorf("refresh task for re-block: %w", models.ErrTaskNotFound)
		}
		if fresh.State == models.TaskStateBlocked {
			return *fresh, nil
		}
		updated, retryErr := w.store.UpdateTaskState(ctx, fresh.ID, fresh.UpdatedAt, models.TaskStateBlocked)
		if retryErr == nil {
			return *updated, nil
		}
		if errors.Is(retryErr, models.ErrStateConflict) || errors.Is(retryErr, models.ErrOptimisticLock) {
			slog.Warn("failed to re-block task for pending HITL; will retry on next pass", "task_id", task.ID, "error", retryErr)
			return task, nil
		}
		return task, fmt.Errorf("re-block task for pending HITL: %w", retryErr)
	}
	return task, fmt.Errorf("re-block task for pending HITL: %w", err)
}

// runPreTaskElicitation consumes prior answers, skips well-specified tasks, or blocks for human input.
func (w *Worker) runPreTaskElicitation(ctx context.Context, task models.Task, project models.Project) (models.Task, bool, error) {
	if enriched, ok, err := w.tryConsumeElicitationAnswers(ctx, task); err != nil {
		return task, false, err
	} else if ok {
		return enriched, false, nil
	}

	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		return task, false, fmt.Errorf("list elicitation subtasks: %w", err)
	}
	if pending := findPendingClarificationSubtask(children); pending != nil {
		reblocked, err := w.reblockTaskForPendingHITL(ctx, task)
		if err != nil {
			return task, false, err
		}
		return reblocked, true, nil
	}

	if shouldSkipElicitation(task) {
		return task, false, nil
	}

	elicitor := NewElicitor(w.gateway)
	analysis, err := elicitor.Analyze(ctx, task, project, w.buildElicitationFileContext(task, project))
	if err != nil {
		slog.Warn("pre-task elicitation failed; continuing without clarification", "task_id", task.ID, "error", err)
		return task, false, nil
	}
	if analysis == nil || !analysis.NeedsClarification || len(analysis.Questions) == 0 {
		return task, false, nil
	}

	if err := w.requestElicitationFromAgent(ctx, task, analysis.Questions, analysis.Reason); err != nil {
		return task, false, err
	}
	return task, true, nil
}

func (w *Worker) requestElicitationFromAgent(
	ctx context.Context,
	task models.Task,
	questions []ElicitationQuestion,
	contextSummary string,
) error {
	if len(questions) == 0 {
		return fmt.Errorf("elicitation requires at least one question")
	}

	detail := formatElicitationHITLDetail(questions, contextSummary)
	description := FormatForHuman(HITLMessage{
		Summary: "Pre-task clarification needed before execution",
		Action:  "Answer all questions in one comment on this subtask, then mark it COMPLETED.",
		Urgency: "blocking",
		Detail:  detail,
	})

	titleQuestion := questions[0].Question
	comments := []models.Comment{
		elicitationQuestionsComment(task.ID, questions),
		hitlExpiryComment(time.Now().Add(DefaultApprovalTimeout)),
	}
	comments[1].TaskID = task.ID
	_, subtasks, err := blockForClarification(ctx, w.store, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + truncate(titleQuestion, 80),
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}}, comments)
	if err != nil {
		w.emit(ctx, task, "ERROR", fmt.Sprintf("elicitation request failed: %v", err))
		return fmt.Errorf("create elicitation subtask: %w", err)
	}
	if len(subtasks) == 0 {
		return fmt.Errorf("no elicitation subtask created")
	}
	w.emit(ctx, task, "CLARIFICATION_REQUESTED", truncate(titleQuestion, 500))
	return nil
}

func elicitationQuestionsComment(taskID string, questions []ElicitationQuestion) models.Comment {
	payload, _ := json.Marshal(questions)
	return models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlElicitationQuestionsPrefix + string(payload),
	}
}

func recordElicitationQuestions(ctx context.Context, store models.KanbanStore, taskID string, questions []ElicitationQuestion) error {
	payload, err := json.Marshal(questions)
	if err != nil {
		return fmt.Errorf("marshal elicitation questions: %w", err)
	}
	return store.AddComment(ctx, models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlElicitationQuestionsPrefix + string(payload),
	})
}
