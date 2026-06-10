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

type ClarificationMessage struct {
	Question       string    `json:"question"`
	Options        []string  `json:"options,omitempty"`
	ContextSummary string    `json:"context_summary"`
	TaskID         string    `json:"task_id"`
	TaskUpdatedAt  time.Time `json:"task_updated_at"`
	RequestedAt    time.Time `json:"requested_at"`
}

type BlockTaskWithSubtasksAndCommentsStore interface {
	BlockTaskWithSubtasksAndComments(
		ctx context.Context,
		taskID string,
		expectedUpdatedAt time.Time,
		subtasks []models.DraftTask,
		comments []models.Comment,
	) (*models.Task, []models.Task, error)
}

func blockForClarification(
	ctx context.Context,
	store models.KanbanStore,
	taskID string,
	expectedUpdatedAt time.Time,
	subtasks []models.DraftTask,
	comments []models.Comment,
) (*models.Task, []models.Task, error) {
	atomicStore, ok := store.(BlockTaskWithSubtasksAndCommentsStore)
	if !ok {
		return nil, nil, fmt.Errorf("kanban store does not support atomic clarification handoff")
	}
	return atomicStore.BlockTaskWithSubtasksAndComments(ctx, taskID, expectedUpdatedAt, subtasks, comments)
}

func buildClarificationDetail(msg ClarificationMessage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n", msg.Question)
	if len(msg.Options) > 0 {
		b.WriteString("\nOptions:\n")
		for i, opt := range msg.Options {
			fmt.Fprintf(&b, "  %d. %s\n", i+1, opt)
		}
	}
	if msg.ContextSummary != "" {
		fmt.Fprintf(&b, "\nContext: %s\n", msg.ContextSummary)
	}
	return b.String()
}

func (w *Worker) RequestClarificationFromAgent(
	ctx context.Context,
	task models.Task,
	question string,
	options []string,
	contextSummary string,
) error {
	if strings.TrimSpace(question) == "" {
		err := fmt.Errorf("clarification question cannot be empty")
		w.emit(ctx, task, "ERROR", err.Error())
		return err
	}
	msg := ClarificationMessage{
		Question:       question,
		Options:        options,
		ContextSummary: contextSummary,
		TaskID:         task.ID,
		TaskUpdatedAt:  task.UpdatedAt,
		RequestedAt:    time.Now(),
	}
	detail := buildClarificationDetail(msg)

	description := FormatForHuman(HITLMessage{
		Summary: "Clarification needed from human",
		Action:  "Answer the question below. Add your response as a comment on this subtask and mark it COMPLETED.",
		Urgency: "blocking",
		Detail:  detail,
	})

	_, subtasks, err := blockForClarification(ctx, w.store, task.ID, task.UpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + truncate(question, 80),
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}}, []models.Comment{hitlExpiryComment(time.Now().Add(DefaultApprovalTimeout))})
	if err != nil {
		w.emit(ctx, task, "ERROR", fmt.Sprintf("clarification request failed: %v", err))
		return fmt.Errorf("create clarification subtask: %w", err)
	}
	if len(subtasks) == 0 {
		return fmt.Errorf("no clarification subtask created")
	}
	w.emit(ctx, task, "CLARIFICATION_REQUESTED", truncate(question, 500))
	return nil
}

const elicitationEmptyAnswerFallback = "(no comment provided — subtask marked complete without written answer)"

const (
	hitlElicitationQuestionsPrefix = "agentd:hitl:elicitation-questions:"
	hitlElicitationUsedPrefix      = "agentd:hitl:elicitation-used:"
)

func findLatestClarificationSubtask(children []models.Task) *models.Task {
	return findLatestChildByTitlePrefix(children, models.HITLSubtaskTitleClarification)
}

func findPendingClarificationSubtask(children []models.Task) *models.Task {
	var pending *models.Task
	for i := range children {
		child := &children[i]
		if !strings.HasPrefix(child.Title, models.HITLSubtaskTitleClarification) {
			continue
		}
		if models.ChildResolvedForParentUnblock(child.State, child.Title) {
			continue
		}
		if pending == nil || child.UpdatedAt.After(pending.UpdatedAt) {
			pending = child
		}
	}
	return pending
}

func isElicitationConsumed(comments []models.Comment, subtaskID string) bool {
	marker := hitlElicitationUsedPrefix + subtaskID
	for _, c := range comments {
		if strings.HasPrefix(c.Body, marker) {
			return true
		}
	}
	return false
}

func markElicitationUsed(ctx context.Context, store models.KanbanStore, parentID, subtaskID string) error {
	return store.AddComment(ctx, models.Comment{
		TaskID: parentID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlElicitationUsedPrefix + subtaskID,
	})
}

func latestElicitationQuestions(comments []models.Comment) ([]ElicitationQuestion, bool) {
	var latest []ElicitationQuestion
	var found bool
	var latestAt time.Time
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, hitlElicitationQuestionsPrefix) {
			continue
		}
		raw := strings.TrimPrefix(c.Body, hitlElicitationQuestionsPrefix)
		var questions []ElicitationQuestion
		if err := json.Unmarshal([]byte(raw), &questions); err != nil {
			slog.Warn("ignored malformed elicitation questions comment", "comment_id", c.ID, "error", err)
			continue
		}
		if !found || c.CreatedAt.After(latestAt) {
			latest = questions
			latestAt = c.CreatedAt
			found = true
		}
	}
	return latest, found
}

func (w *Worker) tryConsumeElicitationAnswers(ctx context.Context, task models.Task) (models.Task, bool, error) {
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		return task, false, fmt.Errorf("list elicitation children: %w", err)
	}
	clarification := findLatestClarificationSubtask(children)
	if clarification == nil || clarification.State != models.TaskStateCompleted {
		return task, false, nil
	}
	comments, err := w.store.ListComments(ctx, task.ID)
	if err != nil {
		return task, false, fmt.Errorf("list parent comments: %w", err)
	}
	if isElicitationConsumed(comments, clarification.ID) {
		return task, false, nil
	}
	questions, ok := latestElicitationQuestions(comments)
	if !ok || len(questions) == 0 {
		questions = []ElicitationQuestion{{Question: "Clarification"}}
	}
	answer := rejectionReasonFromSubtask(ctx, w.store, clarification.ID)
	if answer == "" {
		answer = elicitationEmptyAnswerFallback
	}
	block := formatClarificationsBlock(questions, answer)
	updatedDesc := appendClarificationsToDescription(task.Description, block)
	updated, err := w.store.UpdateTaskDescription(ctx, task.ID, task.UpdatedAt, updatedDesc)
	if err != nil {
		if errors.Is(err, models.ErrStateConflict) {
			slog.Warn("elicitation description update conflict; retry on next pass", "task_id", task.ID, "error", err)
			return task, false, nil
		}
		return task, false, fmt.Errorf("persist elicitation clarifications: %w", err)
	}
	if err := markElicitationUsed(ctx, w.store, task.ID, clarification.ID); err != nil {
		slog.Warn("failed to mark elicitation as consumed", "task_id", task.ID, "subtask_id", clarification.ID, "error", err)
	}
	return *updated, true, nil
}
