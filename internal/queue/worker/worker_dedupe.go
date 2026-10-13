package worker

import (
	"context"
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
