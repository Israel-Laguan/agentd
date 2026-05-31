package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

type CancelRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

func NewCancelRegistry() *CancelRegistry {
	return &CancelRegistry{cancels: make(map[string]context.CancelFunc)}
}

func (c *CancelRegistry) Register(taskID string, cancel context.CancelFunc) {
	if c == nil || taskID == "" || cancel == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancels[taskID] = cancel
}

func (c *CancelRegistry) Deregister(taskID string) {
	if c == nil || taskID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cancels, taskID)
}

func (c *CancelRegistry) Cancel(taskID string) bool {
	if c == nil || taskID == "" {
		return false
	}
	c.mu.Lock()
	cancel, ok := c.cancels[taskID]
	c.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
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
	ExitCode   *int   `json:"ExitCode"`
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
	if env.Error != "" || env.FatalError != "" {
		return -1
	}
	if env.Success != nil && !*env.Success {
		if env.ExitCode != nil {
			return *env.ExitCode
		}
		return -1
	}
	if env.ExitCode != nil {
		return *env.ExitCode
	}
	return 0
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

func (w *Worker) isPromptHang(result sandbox.Result, err error) bool {
	return result.TimedOut && errors.Is(err, models.ErrExecutionTimeout) && safety.DetectPrompt(result.Stdout, result.Stderr).Detected
}

func (w *Worker) isPermissionFailure(result sandbox.Result, err error) bool {
	failed := err != nil || !result.Success
	return failed && safety.DetectPermission(result.Stdout, result.Stderr).Detected
}

func (w *Worker) commitText(ctx context.Context, task models.Task, content string) {
	w.commitTextWithProfile(ctx, task, content, nil)
}

func (w *Worker) handleIterationExceeded(ctx context.Context, task models.Task) {
	payload := "task exceeded maximum tool iterations without producing a final result"
	w.handleAgentFailure(ctx, task, payload)
}

// providerSupportsAgentic returns true if the provider supports agentic mode
// (tool round-tripping with message accumulation).
func (w *Worker) providerSupportsAgentic(profile models.AgentProfile) bool {
	return gateway.ProviderSupportsChatTools(w.gateway, profile.Provider)
}

// SetSandbox swaps the executor used by integration tests that replace the sandbox.
func (w *Worker) SetSandbox(sb sandbox.Executor) {
	w.sandbox = sb
}

// warnIfWorkspaceEmpty emits a WARNING event when the project workspace
// directory exists but contains no files. This serves as a safety net
// alerting operators that the workspace was not seeded before dispatch.
func (w *Worker) warnIfWorkspaceEmpty(ctx context.Context, task models.Task, project *models.Project) {
	if project == nil || project.WorkspacePath == "" {
		return
	}
	wsPath := project.WorkspacePath
	if !filepath.IsAbs(wsPath) {
		return
	}
	entries, err := os.ReadDir(wsPath)
	if err != nil {
		return
	}
	if len(entries) > 0 {
		return
	}
	slog.Warn("workspace empty at dispatch time",
		"task_id", task.ID, "project_id", task.ProjectID)
	if w.sink != nil {
		_ = w.sink.Emit(ctx, models.Event{
			ProjectID: task.ProjectID,
			TaskID:    sql.NullString{String: task.ID, Valid: true},
			Type:      models.EventTypeWarning,
			Payload:   "workspace empty at dispatch time; consider using source_path on materialize or calling POST /workspace/ready after seeding",
		})
	}
}
