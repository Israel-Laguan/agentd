package worker

import (
	"context"
	"errors"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// handleFailedSandboxResult routes a failed sandbox result. An explicit
// bubblewrap with no usable jail is terminal: the model cannot fix a missing
// jail, so the task goes to the same FAILED path as a healing cap, with the
// reason recorded, and skips the retry budget and SANDBOX_VIOLATION feedback.
func (w *Worker) handleFailedSandboxResult(ctx context.Context, task models.Task, result sandbox.Result, err error) {
	if errors.Is(err, models.ErrSandboxUnavailable) {
		w.failTerminal(ctx, task, err, models.TaskStateFailed)
		return
	}
	w.handleAgentFailure(ctx, task, failurePayload(result, err))
}
