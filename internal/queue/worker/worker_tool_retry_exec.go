package worker

import (
	"context"
	"time"
)

// executeToolWithRetry runs body with a per-attempt timeout and optionally
// retries via RetryingExecutor. Transient retryability is decided inside
// RetryingExecutor.shouldRetry; non-retry paths return classify results unchanged.
func (w *Worker) executeToolWithRetry(
	ctx context.Context,
	callID string,
	timeout time.Duration,
	retry bool,
	body func(toolCtx context.Context) ToolResult,
) ToolResult {
	runAttempt := func(attemptCtx context.Context) ToolResult {
		toolCtx, cancel := context.WithTimeout(attemptCtx, timeout)
		defer cancel()
		tr := body(toolCtx)
		if toolCtx.Err() == context.DeadlineExceeded && attemptCtx.Err() == nil {
			return timeoutToolResult(callID, timeout)
		}
		return tr
	}
	if retry && w.toolRetrier != nil {
		return w.toolRetrier.Execute(ctx, runAttempt)
	}
	return runAttempt(ctx)
}
