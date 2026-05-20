package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

const (
	hitlElicitationQuestionsPrefix = "agentd:hitl:elicitation-questions:"
	hitlElicitationUsedPrefix       = "agentd:hitl:elicitation-used:"
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
		return task, false, nil
	}
	block := formatClarificationsBlock(questions, answer)
	updatedDesc := appendClarificationsToDescription(task.Description, block)
	updated, err := w.store.UpdateTaskDescription(ctx, task.ID, task.UpdatedAt, updatedDesc)
	if err != nil {
		return task, false, fmt.Errorf("persist elicitation clarifications: %w", err)
	}
	if err := markElicitationUsed(ctx, w.store, task.ID, clarification.ID); err != nil {
		return task, false, err
	}
	return *updated, true, nil
}
