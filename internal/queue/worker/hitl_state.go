package worker

import (
	"context"
	"strings"
	"time"

	"agentd/internal/models"
)

const (
	hitlApprovalUsedPrefix          = "agentd:hitl:approval-used:"
	hitlApprovalRejectionUsedPrefix = "agentd:hitl:approval-rejection-used:"
)

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
