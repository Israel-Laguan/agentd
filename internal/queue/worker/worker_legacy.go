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
	Command    string                  `json:"command,omitempty"`
	TooComplex bool                    `json:"too_complex,omitempty"`
	Subtasks   []planning.SplitSubtask `json:"subtasks,omitempty"`
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
- Prefer redirects and pipes over embedding output in command arguments.
- The project workspace is the current directory: use relative paths (e.g. lscpu > cpu.txt), never /workspace or any other absolute path.
- Commands run inside the project workspace and may not touch absolute paths outside it (except /dev/null), so never read /etc, /proc or /sys files.
- For host facts (OS, kernel, CPU, memory, disks) run commands that need no paths: uname -a, hostnamectl, lsb_release -a, lscpu, free -h, lsblk, df -h. A report may state only values printed by commands you ran: save each command's output to a file (e.g. uname -a > os.txt) so a later task can build report.md with cat; never type the facts yourself.`

func legacyJSONCommandSystemContent(profile models.AgentProfile) string {
	if !profile.SystemPrompt.Valid {
		return legacyJSONCommandSystemSentinel + legacyJSONCommandSystemBase
	}
	prompt := profile.SystemPrompt.String
	if strings.Contains(strings.ToLower(prompt), "json") {
		return prompt
	}
	// A prose-only prompt (the agentic default) gives the legacy path nothing to
	// answer in, and the reply is decoded as a workerResponse.
	return prompt + "\n\n" + legacyJSONCommandSystemSentinel + legacyJSONCommandSystemBase
}

func workerMessages(task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	system := legacyJSONCommandSystemContent(profile)
	user := fmt.Sprintf("You are executing Task: %s\nDescription: %s\nWorking directory: the current directory is the project workspace; use relative file names only.", task.Title, task.Description)
	return []gateway.PromptMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}
}

// routeLegacyProfile applies complexity routing for legacy JSON command mode.
func (w *Worker) routeLegacyProfile(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) models.AgentProfile {
	messages := w.seedMessages(ctx, task, project, profile)
	messages, _ = w.PrependReviewRejectionFeedback(ctx, task, messages)
	return w.ApplyModelRouting(task, profile, messages, nil)
}

func (w *Worker) command(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (workerResponse, int, gateway.UsageDetails, error) {
	messages := w.seedMessages(ctx, task, project, profile)
	messages, _ = w.PrependReviewRejectionFeedback(ctx, task, messages)
	messages = w.PrependSandboxViolationFeedback(ctx, task, messages)
	req := gateway.AIRequest{
		Purpose:     gateway.PurposeExecLegacyCommand,
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
	req = w.ApplyTuning(req, task, profile, 0)
	resp, tokenUsage, details, err := gateway.GenerateJSONWithUsage[workerResponse](ctx, w.gateway, req)
	if err != nil {
		return workerResponse{}, tokenUsage, details, err
	}
	w.RecordProviderSuccess(ctx, task)
	return resp, tokenUsage, details, nil
}

const legacyPreflightUserNote = `LEGACY MODE CONSTRAINT: respond with exactly one JSON object containing a single shell command.
Use redirects and pipes to write files (e.g. find, du, tee). Do not embed large file contents in the command string.`

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
