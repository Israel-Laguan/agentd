package worker

import (
	"context"
	"log/slog"
	"strings"

	"agentd/internal/models"
)

func (w *Worker) ingestHumanCorrections(ctx context.Context, taskID string, cm *ContextManager) {
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
