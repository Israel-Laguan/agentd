package worker

import (
	"context"
	"encoding/json"
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
	if d := strings.TrimSpace(task.Description); d != "" {
		parts = append(parts, truncateRunes(d, 2000))
	}
	return strings.Join(parts, "\n")
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
		return task, true, nil
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
	if err := recordElicitationQuestions(ctx, w.store, task.ID, questions); err != nil {
		return err
	}

	detail := formatElicitationHITLDetail(questions, contextSummary)
	description := FormatForHuman(HITLMessage{
		Summary: "Pre-task clarification needed before execution",
		Action:  "Answer all questions in one comment on this subtask, then mark it COMPLETED.",
		Urgency: "blocking",
		Detail:  detail,
	})

	titleQuestion := questions[0].Question
	_, subtasks, err := w.store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + truncate(titleQuestion, 80),
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		w.emit(ctx, task, "ERROR", fmt.Sprintf("elicitation request failed: %v", err))
		return fmt.Errorf("create elicitation subtask: %w", err)
	}
	if len(subtasks) == 0 {
		return fmt.Errorf("no elicitation subtask created")
	}
	if err := recordHITLExpiry(ctx, w.store, task.ID, time.Now().Add(DefaultApprovalTimeout)); err != nil {
		return fmt.Errorf("record elicitation expiry: %w", err)
	}
	w.emit(ctx, task, "CLARIFICATION_REQUESTED", truncate(titleQuestion, 500))
	return nil
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
