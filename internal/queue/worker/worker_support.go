package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
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

// toolExecEnvelope is used to parse tool execution results from JSON.
type toolExecEnvelope struct {
	Success    *bool  `json:"Success"`
	ExitCode   int    `json:"ExitCode"`
	Stdout     string `json:"Stdout"`
	Stderr     string `json:"Stderr"`
	Error      string `json:"error"`
	FatalError string `json:"FatalError"`
}

// parseToolEnv attempts to parse the tool result as a toolExecEnvelope.
// Returns the envelope and nil error if JSON is valid, or nil and the error otherwise.
func parseToolEnv(result string) (*toolExecEnvelope, error) {
	var env toolExecEnvelope
	if err := json.Unmarshal([]byte(result), &env); err != nil {
		return nil, err
	}
	return &env, nil
}

// parseToolExitCode determines the exit code from a tool result string.
// Returns -1 for errors or failed executions, 0 otherwise.
func parseToolExitCode(result string) int {
	env, err := parseToolEnv(result)
	if err != nil {
		// Fallback behavior for non-JSON tool results
		if strings.HasPrefix(result, `{"error"`) || strings.HasPrefix(result, `{"FatalError"`) {
			return -1
		}
		if strings.HasPrefix(result, `{"Success":false`) {
			return -1
		}
		return 0
	}
	// Successfully parsed JSON
	if env.Error != "" || env.FatalError != "" || (env.Success != nil && !*env.Success) {
		return -1
	}
	return env.ExitCode
}

func (w *Worker) heartbeatLoop(ctx context.Context, taskID string) {
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.store.UpdateTaskHeartbeat(ctx, taskID); err != nil && ctx.Err() == nil {
				slog.Error("task heartbeat update failed", "task_id", taskID, "error", err)
			}
		}
	}
}

func (w *Worker) registerCancel(taskID string, cancel context.CancelFunc) {
	if w.canceller != nil {
		w.canceller.Register(taskID, cancel)
	}
}

func (w *Worker) deregisterCancel(taskID string) {
	if w.canceller != nil {
		w.canceller.Deregister(taskID)
	}
}

func (w *Worker) loadContext(
	ctx context.Context,
	task models.Task,
) (*models.Project, *models.AgentProfile, error) {
	project, err := w.store.GetProject(ctx, task.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	profile, err := w.store.GetAgentProfile(ctx, task.AgentID)
	return project, profile, err
}

func (w *Worker) command(ctx context.Context, task models.Task, profile models.AgentProfile) (workerResponse, error) {
	messages := w.seedMessages(ctx, task, profile)
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
	req = w.applyTuning(req, task, profile)
	resp, err := gateway.GenerateJSON[workerResponse](ctx, w.gateway, req)
	if err != nil {
		return workerResponse{}, err
	}
	return resp, nil
}

func (w *Worker) applyTuning(req gateway.AIRequest, task models.Task, profile models.AgentProfile) gateway.AIRequest {
	if w.tuner == nil || task.RetryCount <= 0 {
		return req
	}
	action := w.tuner.ForAttempt(task.RetryCount, profile)
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

func (w *Worker) isPromptHang(result sandbox.Result, err error) bool {
	return result.TimedOut && errors.Is(err, models.ErrExecutionTimeout) && safety.DetectPrompt(result.Stdout, result.Stderr).Detected
}

func (w *Worker) isPermissionFailure(result sandbox.Result, err error) bool {
	failed := err != nil || !result.Success
	return failed && safety.DetectPermission(result.Stdout, result.Stderr).Detected
}

func (w *Worker) commitText(ctx context.Context, task models.Task, content string) {
	result := sandbox.Result{
		Success: true,
		Stdout:  content,
	}
	w.commit(ctx, task, result, nil)
}

func (w *Worker) handleIterationExceeded(ctx context.Context, task models.Task) {
	payload := "task exceeded maximum tool iterations without producing a final result"
	w.handleAgentFailure(ctx, task, payload)
}

// providerSupportsAgentic returns true if the provider supports agentic mode
// (tool round-tripping with message accumulation).
func (w *Worker) providerSupportsAgentic(profile models.AgentProfile) bool {
	for _, p := range agenticProviders {
		if strings.EqualFold(profile.Provider, string(p)) {
			return true
		}
	}
	return false
}

// SetSandbox swaps the executor used by integration tests that replace the sandbox.
func (w *Worker) SetSandbox(sb sandbox.Executor) {
	w.sandbox = sb
}