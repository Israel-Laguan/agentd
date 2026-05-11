package worker

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"sync"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
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

func isLegacyJSONCommandSystemPrompt(content string) bool {
	return strings.Contains(content, legacyJSONCommandSystemSentinel)
}

func isMemoryLessonsSystem(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), "LESSONS LEARNED")
}

func workerMessages(task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	system := legacyJSONCommandSystemSentinel + `, {"command":"..."}, or if the task is too complex for one command, {"too_complex":true,"subtasks":[{"title":"...","description":"..."}]}.
Only use subtasks when they are smaller, independently executable units of work. Always use non-interactive flags. Examples: -y, --yes, --assume-yes, --non-interactive, DEBIAN_FRONTEND=noninteractive for apt. Never generate commands that prompt for user input, confirmation, or passwords. Never use sudo or run commands requiring root privileges.`
	if profile.SystemPrompt.Valid {
		system = profile.SystemPrompt.String
	}
	user := fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description)
	return []gateway.PromptMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}
}

func (w *Worker) payload(task models.Task, project models.Project, command string) sandbox.Payload {
	return sandbox.Payload{
		TaskID:        task.ID,
		ProjectID:     task.ProjectID,
		WorkspacePath: project.WorkspacePath,
		Command:       command,
		EnvVars:       BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
		WallTimeout:   w.sandboxWallTimeout,
	}
}

// BuildSandboxEnv assembles environment variable pairs for sandbox execution.
func BuildSandboxEnv(allowlist, extra []string) []string {
	allowed := map[string]struct{}{}
	for _, key := range allowlist {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	env := make([]string, 0, len(allowed)+len(extra))
	for _, pair := range os.Environ() {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if _, ok := allowed[parts[0]]; ok {
			env = append(env, pair)
		}
	}
	for _, pair := range extra {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		env = append(env, pair)
	}
	return env
}

func (w *Worker) recoverPanic(ctx context.Context, task models.Task) {
	if recovered := recover(); recovered != nil {
		w.emit(ctx, task, "PANIC", fmt.Sprintf("worker panic: %v", recovered))
		w.failHard(ctx, task, fmt.Errorf("worker panic: %v", recovered))
	}
}

func (w *Worker) startHeartbeat(ctx context.Context, taskID string) func() {
	heartbeatCtx, stop := context.WithCancel(ctx)
	var hbWg sync.WaitGroup
	hbWg.Add(1)
	go func() {
		defer hbWg.Done()
		w.heartbeatLoop(heartbeatCtx, taskID)
	}()
	return func() {
		stop()
		hbWg.Wait()
	}
}

func (w *Worker) emit(ctx context.Context, task models.Task, kind, payload string) {
	if w.sink == nil {
		return
	}
	_ = w.sink.Emit(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventType(kind),
		Payload:   payload,
	})
}
