package worker

import (
	"context"
	"fmt"
	"strings"

	agentruntime "agentd/internal/agent/runtime"
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

func (w *Worker) command(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (workerResponse, int, error) {
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
	resp, tokenUsage, err := gateway.GenerateJSONWithUsage[workerResponse](ctx, w.gateway, req)
	if err != nil {
		return workerResponse{}, tokenUsage, err
	}
	return resp, tokenUsage, nil
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

const legacyPreflightUserNote = `LEGACY MODE CONSTRAINT: respond with exactly one JSON object containing a single shell command.
Use redirects and pipes to write files (e.g. find, du, tee). Do not embed large file contents in the command string.`

const legacyBreakdownDepthSanityCap = 32

// legacyBreakdownDepth returns how many parent tasks lie above taskID in the breakdown tree.
func (w *Worker) legacyBreakdownDepth(ctx context.Context, taskID string) (int, error) {
	depth := 0
	current := taskID
	for depth < legacyBreakdownDepthSanityCap {
		parents, err := w.store.ListParentTasks(ctx, current)
		if err != nil {
			return 0, err
		}
		if len(parents) == 0 {
			return depth, nil
		}
		depth++
		current = parents[0].ID
	}
	return depth, fmt.Errorf("legacy breakdown depth exceeded sanity cap %d", legacyBreakdownDepthSanityCap)
}

func (w *Worker) legacyDispatchRejectReason(task models.Task, _ models.AgentProfile) string {
	score := (agentruntime.ComplexityScorer{}).ScoreTask(task)
	if w.legacyRejectScore > 0 && score >= w.legacyRejectScore {
		return fmt.Sprintf(
			"Task complexity score %d exceeds legacy reject threshold %d; agentic mode is required.",
			score, w.legacyRejectScore,
		)
	}
	if w.legacyMaxDescriptionLen > 0 && len(task.Description) > w.legacyMaxDescriptionLen {
		return fmt.Sprintf(
			"Task description length %d exceeds legacy limit %d; agentic mode is required.",
			len(task.Description), w.legacyMaxDescriptionLen,
		)
	}
	return ""
}

func (w *Worker) legacySeedMessages(task models.Task, _ models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	messages := workerMessages(task, profile)
	if w.legacyPreflightScore > 0 && !w.providerSupportsAgentic(profile) && !profile.SystemPrompt.Valid {
		if (agentruntime.ComplexityScorer{}).ScoreTask(task) >= w.legacyPreflightScore {
			messages = append(messages, gateway.PromptMessage{
				Role:    "user",
				Content: legacyPreflightUserNote,
			})
		}
	}
	return messages
}

func (w *Worker) rejectLegacyBreakdown(ctx context.Context, task models.Task, cause string, healingCap bool) {
	if healingCap {
		w.createHealingHandoff(ctx, task, planning.HealingAction{
			Type:     planning.HealingActionHuman,
			StepName: planning.HealingStepHumanHandoff,
			Reason:   cause,
		}, cause)
		return
	}
	w.createLegacyModeHandoff(ctx, task, cause, nil)
}

func (w *Worker) handleLegacyTaskBreakdown(ctx context.Context, task models.Task, subtasks []workerSubtask, healingCap bool) {
	if len(subtasks) == 0 {
		w.handleAgentFailure(ctx, task, "worker reported task too complex without subtasks")
		return
	}
	if w.legacyMaxSubtasksPerBreakdown > 0 && len(subtasks) > w.legacyMaxSubtasksPerBreakdown {
		cause := fmt.Sprintf("Model returned %d subtasks but legacy mode allows at most %d per breakdown.",
			len(subtasks), w.legacyMaxSubtasksPerBreakdown)
		w.rejectLegacyBreakdown(ctx, task, cause, healingCap)
		return
	}
	if w.legacyMaxBreakdownDepth > 0 {
		depth, err := w.legacyBreakdownDepth(ctx, task.ID)
		if err != nil {
			cause := fmt.Sprintf("Legacy breakdown sanity check failed: %v", err)
			w.rejectLegacyBreakdown(ctx, task, cause, healingCap)
			return
		}
		if depth >= w.legacyMaxBreakdownDepth {
			cause := fmt.Sprintf("Legacy breakdown depth %d reached limit %d; further decomposition is not supported.",
				depth, w.legacyMaxBreakdownDepth)
			w.rejectLegacyBreakdown(ctx, task, cause, healingCap)
			return
		}
	}
	w.handleTaskBreakdown(ctx, task, subtasks)
}
