package worker

import (
	"context"
	"errors"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// executeRecorded runs command in the sandbox and records it as a COMMAND
// event unless the sandbox refused it. Every path that executes a task's
// command goes through here so reported values trace back to what ran (B-037).
// A command that reached a backend also gets one SANDBOX_BACKEND event beside
// its COMMAND, so the record of which isolation ran it lives outside COMMAND's
// payload.
func (w *Worker) executeRecorded(ctx context.Context, task models.Task, project models.Project, command string) (sandbox.Result, error) {
	result, err := w.sandbox.Execute(ctx, w.payload(task, project, command))
	if !errors.Is(err, models.ErrSandboxViolation) && !errors.Is(err, models.ErrSandboxUnavailable) {
		// Refused commands leave a SANDBOX_VIOLATION (or, with no usable jail, nothing); this one ran.
		w.Emit(ctx, task, string(models.EventTypeCommand), w.executedCommandPayload(command))
		if result.Backend != "" {
			w.Emit(ctx, task, string(models.EventTypeSandboxBackend), result.Backend)
		}
	}
	return result, err
}

// maxCommandEventRunes bounds how much of an executed command is stored in its COMMAND event.
const maxCommandEventRunes = 1000

func (w *Worker) executedCommandPayload(command string) string {
	if w.sandboxScrubber != nil {
		command = w.sandboxScrubber.Scrub(command)
	}
	if runes := []rune(command); len(runes) > maxCommandEventRunes {
		return string(runes[:maxCommandEventRunes]) + "..."
	}
	return command
}
