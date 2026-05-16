package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

const (
	hitlExpiresAtPrefix          = "agentd:hitl:expires-at:"
	hitlApprovalUsedPrefix       = "agentd:hitl:approval-used:"
	hitlDraftReviewCommentPrefix = "agentd:hitl:draft-review\n"
	hitlReviewUsedPrefix         = "agentd:hitl:review-used:"

	approvalSubtaskTitlePrefix = "Approve tool call: "
	reviewSubtaskTitlePrefix   = "Review required:"
)

func approvalSubtaskTitle(toolName string) string {
	return approvalSubtaskTitlePrefix + toolName
}

func recordHITLExpiry(ctx context.Context, store models.KanbanStore, taskID string, expiresAt time.Time) error {
	return store.AddComment(ctx, models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlExpiresAtPrefix + expiresAt.UTC().Format(time.RFC3339),
	})
}

func parseHITLExpiry(comments []models.Comment) (time.Time, bool) {
	var latest time.Time
	var found bool
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, hitlExpiresAtPrefix) {
			continue
		}
		raw := strings.TrimPrefix(c.Body, hitlExpiresAtPrefix)
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		if !found || t.After(latest) {
			latest = t
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

func findLatestApprovalSubtask(children []models.Task, toolName string) *models.Task {
	return findLatestChildByTitlePrefix(children, approvalSubtaskTitle(toolName))
}

func findLatestReviewSubtask(children []models.Task) *models.Task {
	return findLatestChildByTitlePrefix(children, reviewSubtaskTitlePrefix)
}

func isReviewConsumed(comments []models.Comment, subtaskID string) bool {
	marker := hitlReviewUsedPrefix + subtaskID
	for _, c := range comments {
		if strings.HasPrefix(c.Body, marker) {
			return true
		}
	}
	return false
}

func markReviewUsed(ctx context.Context, store models.KanbanStore, parentID, subtaskID string) error {
	return store.AddComment(ctx, models.Comment{
		TaskID: parentID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlReviewUsedPrefix + subtaskID,
	})
}

func findLatestDraftReview(comments []models.Comment) (string, bool) {
	var draft string
	var found bool
	var latest time.Time
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, hitlDraftReviewCommentPrefix) {
			continue
		}
		if !found || c.CreatedAt.After(latest) {
			draft = strings.TrimPrefix(c.Body, hitlDraftReviewCommentPrefix)
			latest = c.CreatedAt
			found = true
		}
	}
	return draft, found
}

func rejectionReasonFromSubtask(ctx context.Context, store models.KanbanStore, subtaskID string) string {
	comments, err := store.ListComments(ctx, subtaskID)
	if err != nil {
		return ""
	}
	for i := len(comments) - 1; i >= 0; i-- {
		c := comments[i]
		if c.Author == models.CommentAuthorUser || c.Author == models.CommentAuthorFrontdesk {
			return strings.TrimSpace(c.Body)
		}
	}
	return ""
}

func persistDraftReviewComment(ctx context.Context, store models.KanbanStore, taskID, draft string) error {
	if strings.TrimSpace(draft) == "" {
		return nil
	}
	return store.AddComment(ctx, models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   hitlDraftReviewCommentPrefix + draft,
	})
}

// tryFinalizeApprovedReview commits a human-approved draft when a completed
// review subtask exists and the draft has not yet been consumed.
func (w *Worker) tryFinalizeApprovedReview(ctx context.Context, task models.Task) (bool, error) {
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		return false, fmt.Errorf("list review subtasks: %w", err)
	}
	review := findLatestReviewSubtask(children)
	if review == nil || review.State != models.TaskStateCompleted {
		return false, nil
	}
	comments, err := w.store.ListComments(ctx, task.ID)
	if err != nil {
		return false, fmt.Errorf("list task comments: %w", err)
	}
	if isReviewConsumed(comments, review.ID) {
		return false, nil
	}
	draft, ok := findLatestDraftReview(comments)
	if !ok || strings.TrimSpace(draft) == "" {
		return false, nil
	}
	if err := markReviewUsed(ctx, w.store, task.ID, review.ID); err != nil {
		return false, err
	}
	result := sandbox.Result{Success: true, Stdout: draft}
	w.commit(ctx, task, result, nil)
	return true, nil
}
