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

type ClarificationResponse struct {
	Answer   string `json:"answer"`
	Selected string `json:"selected,omitempty"`
}

type ClarificationInterface interface {
	RequestClarification(ctx context.Context, msg ClarificationMessage) (ClarificationResponse, error)
}

type BlockingClarificationHandler struct {
	store models.KanbanStore
}

func NewBlockingClarificationHandler(store models.KanbanStore) *BlockingClarificationHandler {
	return &BlockingClarificationHandler{store: store}
}

func (h *BlockingClarificationHandler) RequestClarification(ctx context.Context, msg ClarificationMessage) (ClarificationResponse, error) {
	detail := buildClarificationDetail(msg)

	description := FormatForHuman(HITLMessage{
		Summary: "Clarification needed from human",
		Action:  "Answer the question below. Add your response as a comment on this subtask and mark it COMPLETED.",
		Urgency: "blocking",
		Detail:  detail,
	})

	_, subtasks, err := h.store.BlockTaskWithSubtasks(ctx, msg.TaskID, msg.TaskUpdatedAt, []models.DraftTask{{
		Title:       models.HITLSubtaskTitleClarification + truncate(msg.Question, 80),
		Description: description,
		Assignee:    models.TaskAssigneeHuman,
	}})
	if err != nil {
		return ClarificationResponse{}, fmt.Errorf("create clarification subtask: %w", err)
	}
	if err := recordHITLExpiry(ctx, h.store, msg.TaskID, time.Now().Add(DefaultApprovalTimeout)); err != nil {
		return ClarificationResponse{}, fmt.Errorf("record clarification expiry: %w", err)
	}

	if len(subtasks) == 0 {
		return ClarificationResponse{}, fmt.Errorf("no clarification subtask created")
	}

	return ClarificationResponse{}, nil
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
	handler := NewBlockingClarificationHandler(w.store)
	msg := ClarificationMessage{
		Question:       question,
		Options:        options,
		ContextSummary: contextSummary,
		TaskID:         task.ID,
		TaskUpdatedAt:  task.UpdatedAt,
		RequestedAt:    time.Now(),
	}
	_, err := handler.RequestClarification(ctx, msg)
	if err != nil {
		w.emit(ctx, task, "ERROR", fmt.Sprintf("clarification request failed: %v", err))
		return err
	}
	w.emit(ctx, task, "CLARIFICATION_REQUESTED", truncate(question, 500))
	return nil
}

const elicitationEmptyAnswerFallback = "(no comment provided — subtask marked complete without written answer)"

const (
	hitlElicitationQuestionsPrefix  = "agentd:hitl:elicitation-questions:"
	hitlElicitationUsedPrefix       = "agentd:hitl:elicitation-used:"
	hitlApprovalUsedPrefix          = "agentd:hitl:approval-used:"
	hitlApprovalRejectionUsedPrefix = "agentd:hitl:approval-rejection-used:"
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

func approvalSubtaskTitle(toolName string) string {
	return models.HITLSubtaskTitleApproveTool + toolName
}

func recordHITLExpiry(ctx context.Context, store models.KanbanStore, taskID string, expiresAt time.Time) error {
	return store.AddComment(ctx, models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   models.HITLExpiresAtCommentPrefix + expiresAt.UTC().Format(time.RFC3339),
	})
}

func parseHITLExpiry(comments []models.Comment) (time.Time, bool) {
	var latest time.Time
	var latestAt time.Time
	var found bool
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, models.HITLExpiresAtCommentPrefix) {
			continue
		}
		raw := strings.TrimPrefix(c.Body, models.HITLExpiresAtCommentPrefix)
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		if !found || c.CreatedAt.After(latestAt) {
			latest = t
			latestAt = c.CreatedAt
			found = true
		}
	}
	return latest, found
}

func hitlExpired(comments []models.Comment, now time.Time) bool {
	expiresAt, ok := parseHITLExpiry(comments)
	return ok && now.After(expiresAt)
}

func isApprovalConsumed(comments []models.Comment, subtaskID string) bool {
	marker := hitlApprovalUsedPrefix + subtaskID
	for _, c := range comments {
		if strings.HasPrefix(c.Body, marker) {
			return true
		}
	}
	return false
}

func markApprovalUsed(ctx context.Context, store models.KanbanStore, parentID, subtaskID string) error {
	return store.AddComment(ctx, models.Comment{
		TaskID: parentID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlApprovalUsedPrefix + subtaskID,
	})
}

func isApprovalRejectionConsumed(comments []models.Comment, subtaskID string) bool {
	marker := hitlApprovalRejectionUsedPrefix + subtaskID
	for _, c := range comments {
		if strings.HasPrefix(c.Body, marker) {
			return true
		}
	}
	return false
}

func markApprovalRejectionUsed(ctx context.Context, store models.KanbanStore, parentID, subtaskID string) error {
	return store.AddComment(ctx, models.Comment{
		TaskID: parentID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlApprovalRejectionUsedPrefix + subtaskID,
	})
}

func findLatestChildByTitlePrefix(children []models.Task, prefix string) *models.Task {
	var latest *models.Task
	for i := range children {
		child := &children[i]
		if !strings.HasPrefix(child.Title, prefix) {
			continue
		}
		if latest == nil || child.UpdatedAt.After(latest.UpdatedAt) {
			latest = child
		}
	}
	return latest
}

func findLatestChildByExactTitle(children []models.Task, title string) *models.Task {
	var latest *models.Task
	for i := range children {
		child := &children[i]
		if child.Title != title {
			continue
		}
		if latest == nil || child.UpdatedAt.After(latest.UpdatedAt) {
			latest = child
		}
	}
	return latest
}

func findLatestApprovalSubtask(children []models.Task, toolName string) *models.Task {
	return findLatestChildByExactTitle(children, approvalSubtaskTitle(toolName))
}

func findLatestReviewSubtask(children []models.Task) *models.Task {
	return findLatestChildByTitlePrefix(children, models.HITLSubtaskTitleReview)
}
