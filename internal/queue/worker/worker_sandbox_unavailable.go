package worker

import (
	"context"
	"errors"
	"fmt"

	agenttools "agentd/internal/agent/tools"
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

// handleAgenticToolFailure routes an agentic LoopToolFailure. A tool refused by
// an unavailable required jail is terminal for the same reason as above; a
// no-progress stop goes to a human; any other tool failure keeps the retry
// path (B-052).
func (w *Worker) handleAgenticToolFailure(ctx context.Context, task models.Task, meta LoopMeta, payload string) {
	if meta.ErrorCode == agenttools.ToolErrorCodeSandboxUnavailable {
		w.failTerminal(ctx, task, fmt.Errorf("%w: %s", models.ErrSandboxUnavailable, payload), models.TaskStateFailed)
		return
	}
	if meta.ErrorCode == agenttools.ToolErrorCodeNoProgress {
		// A retry would run the same cycle again (B-081); a human has to fix
		// the cause, e.g. expose the toolchain with sandbox.toolchain_paths.
		w.failTerminal(ctx, task, errors.New(payload), models.TaskStateFailedRequiresHuman)
		return
	}
	w.handleAgentFailure(ctx, task, payload)
}

// isSandboxUnavailableResult reports whether a tool result is a refusal by an
// unavailable required jail.
func isSandboxUnavailableResult(tr agenttools.ToolResult) bool {
	if tr.Error == nil {
		return false
	}
	return tr.Error.Code == agenttools.ToolErrorCodeSandboxUnavailable
}
