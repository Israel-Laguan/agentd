package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// maxSandboxFeedbackEntries bounds how many distinct earlier refusals are
// replayed to the model on a retry.
const maxSandboxFeedbackEntries = 3

// PrependSandboxViolationFeedback appends a user message telling the model
// which of its earlier commands the sandbox refused and why, so a retry does
// not repeat the same command. It returns messages unchanged when the task has
// no recorded SANDBOX_VIOLATION events.
func (w *Worker) PrependSandboxViolationFeedback(
	ctx context.Context,
	task models.Task,
	messages []gateway.PromptMessage,
) []gateway.PromptMessage {
	events, err := w.store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		return messages
	}
	var seen = map[string]bool{}
	var refusals []string
	for i := len(events) - 1; i >= 0 && len(refusals) < maxSandboxFeedbackEntries; i-- {
		payload := strings.TrimSpace(events[i].Payload)
		if events[i].Type != "SANDBOX_VIOLATION" || payload == "" || seen[payload] {
			continue
		}
		seen[payload] = true
		refusals = append(refusals, payload)
	}
	if len(refusals) == 0 {
		return messages
	}
	feedback := fmt.Sprintf(
		"The sandbox refused your earlier command(s):\n- %s\nCommands may only read and write files inside the project workspace: no absolute paths outside it (except /dev/null), no '..', no $HOME or '~/'. Choose a different command that stays inside the project workspace. For host facts use commands that need no paths: uname -a, hostnamectl, lsb_release -a, lscpu, free -h, lsblk, df -h.",
		strings.Join(refusals, "\n- "),
	)
	return append(messages, gateway.PromptMessage{Role: "user", Content: feedback, Section: gateway.SectionComments})
}
