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
const legacyJSONCommandSystemSentinel = "Return JSON with either one safe shell command"

const legacyJSONCommandSystemBase = `, {"command":"..."}, or if the task is too complex for one command, {"too_complex":true,"subtasks":[{"title":"...","description":"..."}]}.
Only use subtasks when they are smaller, independently executable units of work. Always use non-interactive flags. Examples: -y, --yes, --assume-yes, --non-interactive, DEBIAN_FRONTEND=noninteractive for apt. Never generate commands that prompt for user input, confirmation, or passwords. Never use sudo or run commands requiring root privileges.`

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
