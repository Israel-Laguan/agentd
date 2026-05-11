package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
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
	tokenBudget         int
	budgetTracker       spec.BudgetTracker
}

// MemoryRetriever is an optional dependency for pre-fetching durable memories.
type MemoryRetriever interface {
	Recall(ctx context.Context, intent, projectID, userID string) []models.Memory
}

type WorkerOptions struct {
	MaxRetries          int
	MaxToolIterations   int
	TokenBudget         int
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
	if opts.MaxToolIterations <= 0 {
		opts.MaxToolIterations = DefaultMaxToolIterations
	}
	envVars := BuildSandboxEnv(opts.SandboxEnvAllowlist, opts.SandboxExtraEnv)
	var budgetTracker spec.BudgetTracker
	if opts.TokenBudget > 0 {
		budgetTracker = gateway.NewBudgetTracker(opts.TokenBudget)
	}
	return &Worker{
		store: store, gateway: gw, sandbox: sb, breaker: breaker, sink: sink,
		canceller: opts.Canceller, tuner: opts.Tuner, retriever: opts.Retriever,
		heartbeatInterval:   opts.HeartbeatInterval,
		sandboxWallTimeout:  opts.SandboxWallTimeout,
		sandboxEnvAllowlist: append([]string(nil), opts.SandboxEnvAllowlist...),
		sandboxExtraEnv:     append([]string(nil), opts.SandboxExtraEnv...),
		maxRetries:          opts.MaxRetries,
		maxToolIterations:   opts.MaxToolIterations,
		toolExecutor:        NewToolExecutor(sb, "", envVars, opts.SandboxWallTimeout),
		capabilities:        opts.Capabilities,
		tokenBudget:         opts.TokenBudget,
		budgetTracker:       budgetTracker,
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

	messages := w.seedMessages(ctx, task, profile)
	messages = w.buildAgenticMessages(messages, profile)
	tools, toolToAdapter := w.agenticTools(ctx, toolExecutor)

	iterationGuard := NewIterationGuard(w.maxToolIterations)
	budgetGuard := NewBudgetGuard(w.budgetTracker, task.ID)
	deadlineGuard := NewDeadlineGuard(ctx)

	for {
		if err := deadlineGuard.BeforeIteration(); err != nil {
			w.handleGatewayError(ctx, task, err)
			return
		}

		if err := iterationGuard.BeforeIteration(); err != nil {
			w.handleIterationExceeded(ctx, task)
			return
		}

		if iterationGuard.ShouldInjectFinalMessage() {
			messages = append(messages, iterationGuard.FinalMessage())
			iterationGuard.ResetAllowFinal()
		}

		if len(messages) > 40 {
			agenticTruncator := truncation.NewAgenticTruncator(30)
			var err error
			messages, err = agenticTruncator.Apply(ctx, messages, 0)
			if err != nil {
				w.handleGatewayError(ctx, task, err)
				return
			}
		}

		if err := budgetGuard.BeforeCall(); err != nil {
			w.handleGatewayError(ctx, task, err)
			return
		}

		req := gateway.AIRequest{
			Messages:    messages,
			Temperature: profile.Temperature,
			Tools:       tools,
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
			if budgetGuard.IsBudgetExceeded(err) {
				w.handleGatewayError(ctx, task, err)
				return
			}
			w.handleGatewayError(ctx, task, err)
			return
		}

		budgetGuard.AfterCall(resp.TokenUsage)

		messages = append(messages, gateway.PromptMessage{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
		})

		if len(resp.ToolCalls) == 0 {
			w.commitText(ctx, task, resp.Content)
			return
		}

		iterationGuard.AfterIteration(true)

		for _, call := range resp.ToolCalls {
			result := w.executeAgenticTool(cancelCtx, toolExecutor, call, toolToAdapter)
			messages = append(messages, gateway.PromptMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    result,
			})
		}
	}
}

func (w *Worker) agenticTools(ctx context.Context, toolExecutor *ToolExecutor) ([]gateway.ToolDefinition, map[string]string) {
	tools := append([]gateway.ToolDefinition(nil), toolExecutor.Definitions()...)
	if w.capabilities == nil {
		return tools, nil
	}
	capabilityTools, toolToAdapter, err := w.capabilities.GetToolsAndAdapterIndex(ctx)
	if err != nil {
		slog.Warn("failed to get capability tools", "error", err)
		return tools, nil
	}
	return append(tools, capabilityTools...), toolToAdapter
}

func (w *Worker) executeAgenticTool(ctx context.Context, toolExec *ToolExecutor, call gateway.ToolCall, toolToAdapter map[string]string) string {
	switch call.Function.Name {
	case toolNameBash, toolNameRead, toolNameWrite:
		return toolExec.Execute(ctx, call)
	default:
		if w.capabilities == nil {
			return jsonErrorf("unknown tool: %s", call.Function.Name)
		}
		adapterName, ok := toolToAdapter[call.Function.Name]
		if !ok {
			return jsonErrorf("unknown tool: %s", call.Function.Name)
		}
		var args map[string]any
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				return jsonErrorf("invalid arguments: %v", err)
			}
		}
		if args == nil {
			args = map[string]any{}
		}
		out, err := w.capabilities.CallTool(ctx, adapterName, call.Function.Name, args)
		if err != nil {
			return jsonErrorf("capability tool failed: %v", err)
		}
		encoded, err := json.Marshal(out)
		if err != nil {
			return jsonErrorf("capability tool result encode failed: %v", err)
		}
		return string(encoded)
	}
}

func (w *Worker) seedMessages(ctx context.Context, task models.Task, profile models.AgentProfile) []gateway.PromptMessage {
	messages := workerMessages(task, profile)
	if w.retriever == nil {
		return messages
	}
	intent := task.Title + " " + task.Description
	recalled := w.retriever.Recall(ctx, intent, task.ProjectID, "")
	if lessons := memoryFormatLessons(recalled); lessons != "" {
		return append([]gateway.PromptMessage{{Role: "system", Content: lessons}}, messages...)
	}
	return messages
}

func agenticToolUseSystemText() string {
	return `You are an autonomous agent that can execute shell commands, read files, and write files to complete tasks.
When you need to execute a command, use the bash tool.
When you need to read a file, use the read tool.
When you need to create or modify a file, use the write tool.
Return your response as plain text when the task is complete, or use tools to continue working.`
}

func (w *Worker) buildAgenticMessages(messages []gateway.PromptMessage, profile models.AgentProfile) []gateway.PromptMessage {
	toolUse := agenticToolUseSystemText()
	primary := toolUse
	if profile.SystemPrompt.Valid {
		primary = strings.TrimSpace(profile.SystemPrompt.String) + "\n\n" + toolUse
	}

	out := make([]gateway.PromptMessage, 0, len(messages)+1)
	replacedCore := false

	for _, m := range messages {
		if m.Role != "system" {
			out = append(out, m)
			continue
		}
		if isMemoryLessonsSystem(m.Content) {
			out = append(out, m)
			continue
		}
		if isLegacyJSONCommandSystemPrompt(m.Content) {
			if !replacedCore {
				out = append(out, gateway.PromptMessage{Role: "system", Content: primary})
				replacedCore = true
			}
			continue
		}
		if !replacedCore {
			out = append(out, gateway.PromptMessage{Role: "system", Content: primary})
			replacedCore = true
			continue
		}
	}

	if !replacedCore {
		insertIdx := len(out)
		for i, message := range out {
			if message.Role == "user" {
				insertIdx = i
				break
			}
		}
		out = append(out, gateway.PromptMessage{})
		copy(out[insertIdx+1:], out[insertIdx:])
		out[insertIdx] = gateway.PromptMessage{Role: "system", Content: primary}
	}

	return out
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
