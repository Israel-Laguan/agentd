package worker

import (
	"context"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func (w *Worker) commitTextWithProfile(ctx context.Context, task models.Task, content string, profile *models.AgentProfile) {
	if profile != nil && profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.emit(ctx, task, "ERROR", err.Error())
			w.failHard(ctx, task, err)
			return
		} else if done {
			return
		}
		w.createReviewHandoff(ctx, task, content)
		return
	}
	result := sandbox.Result{
		Success: true,
		Stdout:  content,
	}
	w.commit(ctx, task, result, nil)
}
