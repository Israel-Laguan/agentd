package worker

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
)

type workerResponse struct {
	Command    string          `json:"command,omitempty"`
	TooComplex bool            `json:"too_complex,omitempty"`
	Subtasks   []workerSubtask `json:"subtasks,omitempty"`
}

type workerSubtask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

func memoryFormatLessons(memories []models.Memory) string {
	if len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("LESSONS LEARNED (from previous tasks):\n")
	for i, m := range memories {
		if m.Scope == "USER_PREFERENCE" {
			continue
		}
		fmt.Fprintf(&b, "%d. Symptom: %s\n   Solution: %s\n", i+1, m.Symptom.String, m.Solution.String)
	}
	return b.String()
}

// legacyJSONCommandSystemSentinel matches the non-agentic JSON-command worker system prompt.
// It is also used as a probe in tests to detect that the default prompt is in use.
const legacyJSONCommandSystemSentinel = "You are a non-agentic shell executor. Respond with exactly one JSON object"

const legacyJSONCommandSystemBase = ` — no prose, no markdown, no explanation.

CONSTRAINT: the entire response must be valid JSON that fits in a single LLM reply.
Use {"command":"<shell command>"} for tasks that can be completed with one shell command.
Use {"too_complex":true,"subtasks":[{"title":"...","description":"..."}]} ONLY when the
task genuinely decomposes into smaller, independently executable units.

IMPORTANT — do NOT embed large amounts of text or file content inside the command string.
Instead of: {"command":"echo '...500 lines of markdown...' > file.md"}
Do this:    {"command":"find . -type f | sort > REPORT.md"}
Do this:    {"command":"du -sh * | sort -rh > sizes.txt"}

Shell command rules:
- Always use non-interactive flags (-y, --yes, --assume-yes, --non-interactive, DEBIAN_FRONTEND=noninteractive).
- Never generate commands that prompt for user input, confirmation, or passwords.
- Never use sudo or commands requiring root privileges.
- Prefer redirects and pipes over embedding output in command arguments.`

func legacyJSONCommandSystemContent(profile models.AgentProfile) string {
	if profile.SystemPrompt.Valid {
		return profile.SystemPrompt.String
	}
	return legacyJSONCommandSystemSentinel + legacyJSONCommandSystemBase
}

func workerMessages(task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	system := legacyJSONCommandSystemContent(profile)
	user := fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description)
	return []gateway.PromptMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}
}

// routeLegacyProfile applies complexity routing for legacy JSON command mode.
func (w *Worker) routeLegacyProfile(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) models.AgentProfile {
	messages := w.seedMessages(ctx, task, project, profile)
	messages, _ = w.prependReviewRejectionFeedback(ctx, task, messages)
	return w.applyModelRouting(task, profile, messages, nil)
}

func (w *Worker) command(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (workerResponse, error) {
	messages := w.seedMessages(ctx, task, project, profile)
	messages, _ = w.prependReviewRejectionFeedback(ctx, task, messages)
	req := gateway.AIRequest{
		Messages:    messages,
		Temperature: profile.Temperature,
		JSONMode:    true,
		AgentID:     task.AgentID,
		Role:        gateway.RoleWorker,
		TaskID:      task.ID,
		Provider:    profile.Provider,
		Model:       profile.Model,
		MaxTokens:   profile.MaxTokens,
	}
	// Legacy JSON command mode does not execute tool calls; do not advertise tools here.
	req = w.applyTuning(req, task, profile, 0)
	resp, err := gateway.GenerateJSON[workerResponse](ctx, w.gateway, req)
	if err != nil {
		return workerResponse{}, err
	}
	return resp, nil
}

func tuningAttempt(task models.Task, sessionRecoveryGen int) int {
	if sessionRecoveryGen > task.RetryCount {
		return sessionRecoveryGen
	}
	return task.RetryCount
}

func (w *Worker) applyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile, sessionRecoveryGen int) gateway.AIRequest {
	attempt := tuningAttempt(task, sessionRecoveryGen)
	if w.tuner == nil || attempt <= 0 {
		return req
	}
	action := w.tuner.ForAttempt(attempt, profile)
	if action.Type != planning.HealingActionTune {
		return req
	}
	req = w.tuner.Apply(req, action)
	if action.Overrides.Compress {
		req.Messages = append(req.Messages, gateway.PromptMessage{
			Role:    "user",
			Content: "Previous attempts failed. Minimize assumptions, reduce variables, and return the smallest safe next command.",
		})
	}
	return req
}
