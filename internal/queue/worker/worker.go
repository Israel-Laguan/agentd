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
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

// DefaultMaxRetries is the baseline retry budget before eviction.
const DefaultMaxRetries = 3

// agenticProviders lists providers that support agentic mode
// (tool round-tripping with message accumulation).
var agenticProviders = []spec.Provider{
	spec.ProviderOpenAI,
	spec.ProviderAnthropic,
}

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
	sandboxScrubber     sandbox.Scrubber
	maxRetries          int
	maxToolIterations   int
	truncatorMax        int
	truncationThreshold int
	characterBudget     int
	toolExecutor        *ToolExecutor
	capabilities        *capabilities.Registry
	tokenBudget         int
	budgetTracker       spec.BudgetTracker
	hooks               *HookChain
	pluginMounter       PluginMounter
	contextCfg          config.AgenticContextConfig
}

// MemoryRetriever is an optional dependency for pre-fetching durable memories.
type MemoryRetriever interface {
	Recall(ctx context.Context, intent, projectID, userID string) []models.Memory
}

// PluginMounter loads and mounts plugins from a directory into a
// HookChain and capabilities Registry. The worker calls this to
// mount project-scoped plugins (from workspace directories) and
// session-scoped plugins (by name from AgentProfile.Plugins).
type PluginMounter interface {
	MountProject(workspacePath string, chain *HookChain, registry *capabilities.Registry) error
	MountSession(names []string, chain *HookChain, registry *capabilities.Registry) error
}

type WorkerOptions struct {
	MaxRetries              int
	MaxToolIterations       int
	TokenBudget             int
	AgenticTruncatorMax     int
	AgenticTruncationThresh int
	AgenticCharacterBudget  int
	AgenticContext          config.AgenticContextConfig
	Canceller               *CancelRegistry
	Tuner                   *planning.ParameterTuner
	Retriever               MemoryRetriever
	HeartbeatInterval       time.Duration
	SandboxWallTimeout      time.Duration
	SandboxEnvAllowlist     []string
	SandboxExtraEnv         []string
	SandboxScrubPatterns    []string
	Capabilities            *capabilities.Registry
	Hooks                   *HookChain
	PluginMounter           PluginMounter
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
		opts.MaxToolIterations = config.DefaultMaxToolIterations
	}
	if opts.AgenticTruncatorMax <= 0 {
		opts.AgenticTruncatorMax = config.DefaultAgenticTruncatorMax
	}
	if opts.AgenticTruncationThresh <= 0 {
		opts.AgenticTruncationThresh = config.DefaultAgenticTruncationThreshold
	}
	if opts.AgenticCharacterBudget < 0 {
		opts.AgenticCharacterBudget = config.DefaultAgenticCharacterBudget
	}
	envVars := BuildSandboxEnv(opts.SandboxEnvAllowlist, opts.SandboxExtraEnv)
	var budgetTracker spec.BudgetTracker
	if opts.TokenBudget > 0 {
		budgetTracker = gateway.NewBudgetTracker(opts.TokenBudget)
	}
	scrubber := sandbox.NewScrubber(opts.SandboxScrubPatterns)
	base := resolveHooks(opts.Hooks)
	hooks := base.Clone()
	hooks.PrependPost(ScrubResultHook(scrubber))
	hooks.RegisterPost(AuditHook(sink, scrubber))

	return &Worker{
		store: store, gateway: gw, sandbox: sb, breaker: breaker, sink: sink,
		canceller: opts.Canceller, tuner: opts.Tuner, retriever: opts.Retriever,
		heartbeatInterval:   opts.HeartbeatInterval,
		sandboxWallTimeout:  opts.SandboxWallTimeout,
		sandboxEnvAllowlist: append([]string(nil), opts.SandboxEnvAllowlist...),
		sandboxExtraEnv:     append([]string(nil), opts.SandboxExtraEnv...),
		sandboxScrubber:     scrubber,
		maxRetries:          opts.MaxRetries,
		maxToolIterations:   opts.MaxToolIterations,
		truncatorMax:        opts.AgenticTruncatorMax,
		truncationThreshold: opts.AgenticTruncationThresh,
		characterBudget:     opts.AgenticCharacterBudget,
		toolExecutor:        NewToolExecutor(sb, "", envVars, opts.SandboxWallTimeout),
		capabilities:        opts.Capabilities,
		tokenBudget:         opts.TokenBudget,
		budgetTracker:       budgetTracker,
		hooks:               hooks,
		pluginMounter:       opts.PluginMounter,
		contextCfg:          opts.AgenticContext,
	}
}

// Process handles task execution, supporting two modes:
// - Legacy mode (default): single-shot JSON command execution via GenerateJSON
// - Agentic mode: inner loop with tool calling and message accumulation (processAgentic)
// Routing is determined by profile.AgenticMode flag. When agentic mode is enabled but
// the provider doesn't support tool round-tripping, falls back to legacy mode.
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
	// Routing: check AgenticMode flag to determine execution path
	// Agentic mode requires provider support (see agenticProviders)
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

	// Create task-local ToolExecutor to avoid races with concurrent task executions
	taskToolExecutor := NewToolExecutor(
		w.sandbox,
		project.WorkspacePath,
		BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
		w.sandboxWallTimeout,
	)

	taskHooks, taskCaps := w.mountScopedPlugins(project, profile)

	messages := w.seedMessages(ctx, task, profile)
	messages = w.buildAgenticMessages(messages, profile)
	tools, toolToAdapter := w.agenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)

	iterationGuard := NewIterationGuard(w.maxToolIterations)
	budgetGuard := NewBudgetGuard(w.budgetTracker, task.ID)
	deadlineGuard := NewDeadlineGuard(cancelCtx)

	// ContextManager is initialized lazily per task to handle its own cache/state
	contextCfg := w.contextCfg
	if contextCfg.RollingThresholdTurns <= 0 {
		contextCfg.RollingThresholdTurns = config.DefaultRollingThresholdTurns
	}
	if contextCfg.KeepRecentTurns <= 0 {
		contextCfg.KeepRecentTurns = config.DefaultKeepRecentTurns
	}
	if contextCfg.AnchorBudget <= 0 {
		contextCfg.AnchorBudget = config.DefaultAnchorBudget
	}
	if contextCfg.WorkingBudget <= 0 {
		contextCfg.WorkingBudget = config.DefaultWorkingBudget
	}
	if contextCfg.CompressedBudget <= 0 {
		contextCfg.CompressedBudget = config.DefaultCompressedBudget
	}

	cm := NewContextManager(
		contextCfg,
		w.gateway,
		task.AgentID,
		task.ID,
	)

	for {
		shouldContinue, err := w.processAgenticIteration(
			cancelCtx, task, profile, &messages, tools, toolToAdapter, taskToolExecutor,
			iterationGuard, budgetGuard, deadlineGuard, cm,
			taskHooks, taskCaps,
		)
		if err != nil {
			return
		}
		if !shouldContinue {
			return
		}
	}
}

func (w *Worker) processAgenticIteration(
	ctx context.Context, task models.Task, profile models.AgentProfile,
	messages *[]gateway.PromptMessage, tools []gateway.ToolDefinition,
	toolToAdapter map[string]string, toolExecutor *ToolExecutor,
	iterationGuard *IterationGuard, budgetGuard *BudgetGuard,
	deadlineGuard *DeadlineGuard, cm *ContextManager,
	taskHooks *HookChain, _ *capabilities.Registry,
) (bool, error) {
	if err := deadlineGuard.BeforeIteration(); err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}

	if err := iterationGuard.BeforeIteration(); err != nil {
		w.handleIterationExceeded(ctx, task)
		return false, err
	}

	// Ingest human corrections from task comments. Use ContextManager.ShouldPollComments
	// to avoid listing all comments on every iteration.
	// Poll interval chosen to balance responsiveness and DB load.
	const commentPollInterval = 5 * time.Second
	if cm.ShouldPollComments(commentPollInterval) {
		w.ingestHumanCorrections(ctx, task.ID, cm)
	}

	// Replace legacy truncator with ContextManager
	prepared, err := cm.PrepareContext(ctx, *messages)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}
	*messages = prepared

	if iterationGuard.ShouldInjectFinalMessage() {
		*messages = append(*messages, iterationGuard.FinalMessage())
		iterationGuard.ResetAllowFinal()
	}

	if err := budgetGuard.BeforeCall(); err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}

	req := gateway.AIRequest{
		Messages:    *messages,
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

	resp, err := w.gateway.Generate(ctx, req)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return false, err
	}

	budgetGuard.AfterCall(resp.TokenUsage)

	*messages = append(*messages, gateway.PromptMessage{
		Role:      "assistant",
		Content:   resp.Content,
		ToolCalls: append([]gateway.ToolCall(nil), resp.ToolCalls...),
	})

	if len(resp.ToolCalls) == 0 {
		w.commitText(ctx, task, resp.Content)
		return false, nil
	}

	iterationGuard.AfterIteration(true)

	for _, call := range resp.ToolCalls {
		result := w.dispatchToolWithHooks(ctx, task.ID, task.ProjectID, call, toolToAdapter, toolExecutor, taskHooks)

		if detected := cm.CheckToolResult(result); len(detected) > 0 {
			slog.Info("auto-detected context corrections",
				"task_id", task.ID,
				"count", len(detected),
			)
		}

		*messages = append(*messages, gateway.PromptMessage{
			Role:       "tool",
			ToolCallID: call.ID,
			Content:    result,
		})
	}

	return true, nil
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

// DispatchTool is the single entry point for tool execution in the agentic loop.
// It handles both built-in tools (bash, read, write) and capability tools (MCP).
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - call: The tool call from the AI response
//   - toolToAdapter: Map of tool names to adapter names for MCP tools
//
// Returns the tool execution result as a string (JSON-encoded for MCP tools, direct for built-in tools).
func (w *Worker) DispatchTool(ctx context.Context, sessionID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor) string {
	return w.dispatchToolWithProject(ctx, sessionID, "", call, toolToAdapter, toolExecutor)
}

func (w *Worker) dispatchToolWithProject(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor) string {
	hookCtx := HookContext{
		ToolName:  call.Function.Name,
		Args:      call.Function.Arguments,
		CallID:    call.ID,
		SessionID: sessionID,
		ProjectID: projectID,
		Timestamp: time.Now(),
	}

	if w.hooks != nil {
		if verdict := w.hooks.RunPre(hookCtx); verdict.ShortCircuit {
			return verdict.Result
		} else if verdict.Veto && verdict.Result != "" {
			result := verdict.Result
			result = w.hooks.RunPost(hookCtx, result)
			return result
		} else if verdict.Veto {
			return jsonErrorf("tool call vetoed: %s", verdict.Reason)
		}
	}

	var result string
	switch call.Function.Name {
	case toolNameBash, toolNameRead, toolNameWrite:
		result = toolExecutor.Execute(ctx, call)
	default:
		if w.capabilities == nil {
			result = jsonErrorf("unknown tool: %s", call.Function.Name)
		} else if adapterName, ok := toolToAdapter[call.Function.Name]; !ok {
			result = jsonErrorf("unknown tool: %s", call.Function.Name)
		} else {
			var args map[string]any
			if strings.TrimSpace(call.Function.Arguments) != "" {
				if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
					result = jsonErrorf("invalid arguments: %v", err)
				}
			}
			if result == "" {
				if args == nil {
					args = map[string]any{}
				}
				out, err := w.capabilities.CallTool(ctx, adapterName, call.Function.Name, args)
				if err != nil {
					result = jsonErrorf("capability tool failed: %v", err)
				} else {
					encoded, err := json.Marshal(out)
					if err != nil {
						result = jsonErrorf("capability tool result encode failed: %v", err)
					} else {
						result = string(encoded)
					}
				}
			}
		}
	}

	if w.hooks != nil {
		result = w.hooks.RunPost(hookCtx, result)
	}

	return result
}

// executeAgenticTool is a wrapper around DispatchTool for backward compatibility.
// Use DispatchTool directly instead.
func (w *Worker) executeAgenticTool(ctx context.Context, sessionID string, toolExec *ToolExecutor, call gateway.ToolCall, toolToAdapter map[string]string) string {
	if toolExec == nil {
		toolExec = w.toolExecutor
	}
	return w.DispatchTool(ctx, sessionID, call, toolToAdapter, toolExec)
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

func (w *Worker) ingestHumanCorrections(ctx context.Context, taskID string, cm *ContextManager) {
	comments, err := w.store.ListCommentsSince(ctx, taskID, cm.CommentHighWater())
	if err != nil {
		slog.Warn("failed to list task comments for corrections", "task_id", taskID, "error", err)
		return
	}
	defer cm.AdvanceCommentHighWater(comments)
	for _, c := range comments {
		source, ok := correctionSourceForCommentAuthor(c.Author)
		if !ok {
			continue
		}
		if !cm.MarkCommentCorrectionSeen(c) {
			continue
		}
		if rec := ParseCorrectionComment(c.Body, source); rec != nil {
			cm.InjectCorrection(*rec)
		}
	}
}

func correctionSourceForCommentAuthor(author models.CommentAuthor) (CorrectionSource, bool) {
	switch author {
	case models.CommentAuthorUser, models.CommentAuthorFrontdesk:
		return CorrectionSourceHuman, true
	default:
		if strings.EqualFold(string(author), string(CorrectionSourceReviewer)) {
			return CorrectionSourceReviewer, true
		}
		return "", false
	}
}

// SetSandbox swaps the executor used by integration tests that replace the sandbox.
func (w *Worker) SetSandbox(sb sandbox.Executor) {
	w.sandbox = sb
}
