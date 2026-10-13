package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
)

// violationCommandMarker separates the refusal reason from the refused
// command in SANDBOX_VIOLATION payloads (see the emitLine calls in
// internal/sandbox/executor.go: "<reason>; command: <command>").
const violationCommandMarker = "; command: "

// maxViolationCommandRunes mirrors sandbox.truncateCommand: a refused command
// longer than this is cut with a "..." suffix in the violation event, so the
// comparison below truncates the candidate the same way.
const maxViolationCommandRunes = 300

// repeatOfPreviousAttempt reports whether command duplicates the task's most
// recent executed (COMMAND) or refused (SANDBOX_VIOLATION) command. It also
// returns the failure text recorded after that attempt (the latest RETRY
// payload) for the handoff message. A lookup error yields repeat=false so a
// blind store degrades to the old always-retry behavior, never to a false
// eviction.
func (w *Worker) repeatOfPreviousAttempt(ctx context.Context, task models.Task, command string) (prevCommand, lastFailure string, repeat bool) {
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		return "", "", false
	}
	wantExecuted := w.executedCommandPayload(command)
	wantRefused := truncateRefusedCommand(command)
	for _, ev := range events {
		switch string(ev.Type) {
		case string(models.EventTypeCommand):
			if strings.TrimSpace(ev.Payload) != "" {
				prevCommand = ev.Payload
			}
		case "SANDBOX_VIOLATION":
			if cmd, ok := violationCommandOf(ev.Payload); ok {
				prevCommand = cmd
			}
		case "RETRY":
			lastFailure = ev.Payload
		}
	}
	if prevCommand == "" {
		return "", lastFailure, false
	}
	return prevCommand, lastFailure, prevCommand == wantExecuted || prevCommand == wantRefused
}

// rejectUnrunnableCommand applies the B-043 pre-execution guards: an empty
// model command fails without touching the sandbox, and a retry that would
// send the exact command that just failed hands off early via the existing
// eviction path. It reports true when the caller must stop before Execute.
func (w *Worker) rejectUnrunnableCommand(ctx context.Context, task models.Task, command string, audit *legacyTaskAudit) bool {
	if strings.TrimSpace(command) == "" {
		audit.failed = true
		w.handleAgentFailure(ctx, task, "model returned an empty command; nothing was executed")
		return true
	}
	// The RetryCount window keeps the human-retry escape hatch: a
	// post-eviction operator retry runs at RetryCount == maxRetries and still
	// gets one fresh attempt.
	if task.RetryCount > 0 && task.RetryCount < w.maxRetries {
		if prev, failure, repeat := w.repeatOfPreviousAttempt(ctx, task, command); repeat {
			audit.failed = true
			w.evict(ctx, task, fmt.Sprintf("deterministic failure: identical command re-suggested after failure; not re-executed (attempt %d of budget %d).\ncommand: %s\nlast error: %s",
				task.RetryCount+1, w.maxRetries, truncate(prev, 500), truncate(failure, 500)))
			return true
		}
	}
	return false
}

// violationCommandOf extracts the refused command from a SANDBOX_VIOLATION
// payload. It reports false when the payload carries no command suffix.
func violationCommandOf(payload string) (string, bool) {
	_, cmd, found := strings.Cut(payload, violationCommandMarker)
	if !found || strings.TrimSpace(cmd) == "" {
		return "", false
	}
	return cmd, true
}

// truncateRefusedCommand applies the sandbox's violation-event truncation to
// a candidate command so over-long repeats still match their stored suffix.
func truncateRefusedCommand(command string) string {
	runes := []rune(strings.TrimSpace(command))
	if len(runes) <= maxViolationCommandRunes {
		return string(runes)
	}
	return string(runes[:maxViolationCommandRunes]) + "..."
}
