package agentic

import (
	"context"
	"log/slog"
	"strings"

	agentcontext "agentd/internal/agent/context"
	"agentd/internal/models"
)

func (e *Engine) ingestHumanCorrections(ctx context.Context, taskID string, cm *agentcontext.ContextManager) {
	if cm == nil {
		return
	}

	comments, err := e.config.Store.ListCommentsSince(ctx, taskID, cm.CommentHighWater())
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
		if rec := agentcontext.ParseCorrectionComment(c.Body, source); rec != nil {
			cm.InjectCorrection(*rec)
		}
	}
}

func (e *Engine) IngestHumanCorrections(ctx context.Context, store TaskReadStore, taskID string, cm *agentcontext.ContextManager) {
	if cm == nil || store == nil {
		return
	}
	e.config.Store = store
	e.ingestHumanCorrections(ctx, taskID, cm)
}

func correctionSourceForCommentAuthor(author models.CommentAuthor) (agentcontext.CorrectionSource, bool) {
	switch author {
	case models.CommentAuthorUser, models.CommentAuthorFrontdesk:
		return agentcontext.CorrectionSourceHuman, true
	default:
		if strings.EqualFold(string(author), string(agentcontext.CorrectionSourceReviewer)) {
			return agentcontext.CorrectionSourceReviewer, true
		}
		return "", false
	}
}
