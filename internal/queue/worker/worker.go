package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

// DefaultMaxRetries is the baseline retry budget before eviction.
const DefaultMaxRetries = 3

// DefaultMaxToolIterations is the default number of tool call iterations in agentic mode.
const DefaultMaxToolIterations = 10

type Worker struct {
	store               models.KanbanStore
	gateway             gateway.AIGateway
	sandbox             sandbox.Executor
	breaker             *safety.CircuitBreaker
	sink                models.EventSink
	canceller           *CancelRegistry
	tuner               *planning.ParameterTuner
	retriever           MemoryRetriever
	heartbeatInterval   time.Duration
	sandboxWallTimeout  time.Duration
	sandboxEnvAllowlist []string
	sandboxExtraEnv     []string
	maxRetries          int
	maxToolIterations   int
	toolExecutor        *ToolExecutor
	capabilities        *capabilities.Registry
}

// MemoryRetriever is an optional dependency for pre-fetching durable memories.
type MemoryRetriever interface {
	Recall(ctx context.Context, intent, projectID, userID string) []models.Memory
}

type WorkerOptions struct {
	MaxRetries          int
	Canceller           *CancelRegistry
	Tuner               *planning.ParameterTuner
	Retriever           MemoryRetriever
	HeartbeatInterval   time.Duration
	SandboxWallTimeout  time.Duration
	SandboxEnvAllowlist []string
	SandboxExtraEnv     []string
	Capabilities        *capabilities.Registry
}

func NewWorker(
	store models.KanbanStore,
	gw gateway.AIGateway,
	sb sandbox.Executor,
	breaker *safety.CircuitBreaker,
	sink models.EventSink,
	opts WorkerOptions,
) *Worker {
	if opts.MaxRetries < 1 {
		opts.MaxRetries = DefaultMaxRetries
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 30 * time.Second
	}
	if opts.SandboxWallTimeout <= 0 {
		opts.SandboxWallTimeout = 10 * time.Minute
	}
	if len(opts.SandboxEnvAllowlist) == 0 {
		opts.SandboxEnvAllowlist = []string{"PATH", "HOME", "LANG", "LC_ALL", "USER"}
	}
	if len(opts.SandboxExtraEnv) == 0 {
		opts.SandboxExtraEnv = []string{"CI=true", "DEBIAN_FRONTEND=noninteractive", "NO_COLOR=1"}
	}
	envVars := BuildSandboxEnv(opts.SandboxEnvAllowlist, opts.SandboxExtraEnv)
	return &Worker{
		store: store, gateway: gw, sandbox: sb, breaker: breaker, sink: sink,
		canceller: opts.Canceller, tuner: opts.Tuner, retriever: opts.Retriever,
		heartbeatInterval:   opts.HeartbeatInterval,
		sandboxWallTimeout:  opts.SandboxWallTimeout,
		sandboxEnvAllowlist: append([]string(nil), opts.SandboxEnvAllowlist...),
		sandboxExtraEnv:     append([]string(nil), opts.SandboxExtraEnv...),
		maxRetries:          opts.MaxRetries,
		maxToolIterations:   DefaultMaxToolIterations,
		toolExecutor:        NewToolExecutor(sb, "", envVars, opts.SandboxWallTimeout),
		capabilities:        opts.Capabilities,
	}
}

func (w *Worker) Process(ctx context.Context, task models.Task) {
	defer w.recoverPanic(ctx, task)
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		w.failHard(ctx, task, err)
		return
	}
	running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, os.Getpid())
	if err != nil {
		return
	}
	task = *running
	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, w.store))
	stopHeartbeat := w.startHeartbeat(ctx, task.ID)
	defer stopHeartbeat()
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, *project)
		return
	}
	if profile.AgenticMode {
		if w.providerSupportsAgentic(*profile) {
			w.processAgentic(ctx, task, *project, *profile)
			return
		}
		slog.Warn("agentic mode requested but provider does not support tool round-tripping; falling back to legacy mode",
			"task_id", task.ID,
			"provider", profile.Provider,
		)
	}
	response, err := w.command(ctx, task, *profile)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return
	}
	if response.TooComplex {
		w.handleTaskBreakdown(ctx, task, response.Subtasks)
		return
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.registerCancel(task.ID, cancel)
	defer w.deregisterCancel(task.ID)
	command := response.Command
	result, runErr := w.sandbox.Execute(execCtx, w.payload(task, *project, command))
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, *project, command, result)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, command, result)
		return
	}
	w.commit(ctx, task, result, runErr)
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
	messages := workerMessages(task, profile)
	if w.retriever != nil {
		intent := task.Title + " " + task.Description
		recalled := w.retriever.Recall(ctx, intent, task.ProjectID, "")
		if lessons := memoryFormatLessons(recalled); lessons != "" {
			messages = append([]gateway.PromptMessage{{Role: "system", Content: lessons}}, messages...)
		}
	}
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
	if w.capabilities != nil {
		tools, err := w.capabilities.GetTools(ctx)
		if err != nil {
			slog.Warn("failed to get capability tools", "error", err)
		} else {
			req.Tools = tools
		}
	}
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

func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.registerCancel(task.ID, cancel)
	defer w.deregisterCancel(task.ID)

	toolExecutor := NewToolExecutor(
		w.sandbox,
		project.WorkspacePath,
		BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
		w.sandboxWallTimeout,
	)

	messages := w.buildAgenticMessages(task, profile)

	for i := 0; i < w.maxToolIterations; i++ {
		req := gateway.AIRequest{
			Messages:    messages,
			Temperature: profile.Temperature,
			Tools:       toolExecutor.Definitions(),
			AgentID:     task.AgentID,
			Role:        gateway.RoleWorker,
			TaskID:      task.ID,
			Provider:    profile.Provider,
			Model:       profile.Model,
			MaxTokens:   profile.MaxTokens,
		}
		req = w.applyTuning(req, task, profile)

		resp, err := w.gateway.Generate(cancelCtx, req)
		if err != nil {
			w.handleGatewayError(ctx, task, err)
			return
		}

		messages = append(messages, gateway.PromptMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
		})

		if len(resp.ToolCalls) == 0 {
			w.commitText(ctx, task, resp.Content)
			return
		}

		for _, call := range resp.ToolCalls {
			result := toolExecutor.Execute(cancelCtx, call)
			messages = append(messages, gateway.PromptMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}

	w.handleIterationExceeded(ctx, task)
}

func (w *Worker) buildAgenticMessages(task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	system := `You are an autonomous agent that can execute shell commands, read files, and write files to complete tasks.
When you need to execute a command, use the bash tool.
When you need to read a file, use the read tool.
When you need to create or modify a file, use the write tool.
Return your response as plain text when the task is complete, or use tools to continue working.`

	if profile.SystemPrompt.Valid {
		system = profile.SystemPrompt.String
	}
	user := fmt.Sprintf("You are executing Task: %s\nDescription: %s", task.Title, task.Description)
	return []gateway.PromptMessage{{Role: "system", Content: system}, {Role: "user", Content: user}}
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

func (w *Worker) providerSupportsAgentic(profile models.AgentProfile) bool {
	return strings.EqualFold(profile.Provider, string(gateway.ProviderOpenAI))
}

// SetSandbox swaps the executor used by integration tests that replace the sandbox.
func (w *Worker) SetSandbox(sb sandbox.Executor) {
	w.sandbox = sb
}
