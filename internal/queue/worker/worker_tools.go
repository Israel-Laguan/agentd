package worker

import (
	"context"
	"encoding/json"
	"time"

	agenthooks "agentd/internal/agent/hooks"
	agentsubagent "agentd/internal/agent/subagent"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

type delegateArgs struct {
	Subagent string `json:"subagent"`
	Task     string `json:"task"`
}

type delegateParallelArgs struct {
	Tasks []delegateArgs `json:"tasks"`
}

// DispatchTool is the single entry point for tool execution in the agentic loop.
// It handles both built-in tools (bash, read, write) and capability tools (MCP).
// It intentionally does not accept project-scoped capability registries; scoped
// tools are available through the internal agentic dispatch path.
//
// Backward-compatibility note: This public API does not forward scoped
// capabilities. External callers (e.g. tests using DispatchTool directly) will
// not have access to project-scoped capability tools. Callers that need scoped
// capabilities must use the internal path via dispatchToolWithHooks ->
// dispatchToolWithProject, which is what the agentic loop uses.
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - call: The tool call from the AI response
//   - toolToAdapter: Map of tool names to adapter names for MCP tools
//
// Returns a structured ToolResult describing the outcome.
func (w *Worker) DispatchTool(ctx context.Context, sessionID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor) agenttools.ToolResult {
	retry := w.toolRetrier != nil && w.toolRetries.Allows(call.Function.Name)
	return w.dispatchToolWithProject(ctx, sessionID, "", call, toolToAdapter, toolExecutor, nil, retry, nil, nil)
}

// timeoutToolResult returns a structured ToolResult for a timed-out tool.
func timeoutToolResult(callID string, timeout time.Duration) agenttools.ToolResult {
	return agenttools.TimeoutResult(callID, timeout.Milliseconds())
}

// filterAgenticTools applies per-task tool manifest filtering when enabled.
func (w *Worker) filterAgenticTools(
	tools []gateway.ToolDefinition,
	index map[string]string,
	task models.Task,
	profile models.AgentProfile,
) ([]gateway.ToolDefinition, map[string]string) {
	if w.toolManifest == nil {
		return tools, index
	}
	return w.toolManifest.Filter(tools, index, task, profile)
}

func (w *Worker) dispatchToolWithProject(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor, scopedCapabilities *capabilities.Registry, retry bool, callEnv []string, auditParent *agenthooks.HookContext) agenttools.ToolResult {
	timeout := w.toolTimeouts.Lookup(call.Function.Name, config.DefaultToolTimeout)
	return w.executeToolCore(ctx, sessionID, projectID, call, toolToAdapter, toolExecutor, scopedCapabilities, timeout, retry, callEnv, auditParent)
}

func (w *Worker) executeToolCore(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor, scopedCapabilities *capabilities.Registry, timeout time.Duration, retry bool, callEnv []string, auditParent *agenthooks.HookContext) agenttools.ToolResult {
	start := time.Now()
	hookCtx := agenthooks.HookContext{
		ToolName:  call.Function.Name,
		Args:      call.Function.Arguments,
		CallID:    call.ID,
		SessionID: sessionID,
		ProjectID: projectID,
		Timestamp: start,
		ExecCtx:   ctx,
	}
	if auditParent != nil {
		hookCtx.TurnID = auditParent.TurnID
		hookCtx.Verdicts = auditParent.Verdicts
		hookCtx.TokenCountBefore = auditParent.TokenCountBefore
	}

	if w.hooks != nil {
		if verdict := w.hooks.RunPre(hookCtx); verdict.ShortCircuit {
			// Intentionally skips post-hooks (audit, scrub). Hooks that need
			// observability should use Veto+Result without ShortCircuit; see DryRunHook.
			return agenttools.ClassifyPrecomputedToolResult(call.ID, call.Function.Name, verdict.Result, time.Since(start).Milliseconds())
		} else if verdict.Veto && verdict.Result != "" {
			result := verdict.Result
			tr := agenttools.SuccessResult(call.ID, result, time.Since(start).Milliseconds())
			hookCtx.ResultStatus = tr.Status
			hookCtx.ResultStatusSet = true
			tr.Content = w.hooks.RunPost(hookCtx, tr.Content)
			return tr
		} else if verdict.Veto {
			tr := agenttools.VetoedResult(call.ID, verdict.Reason)
			hookCtx.ResultStatus = tr.Status
			hookCtx.ResultStatusSet = true
			tr.Content = w.hooks.RunPost(hookCtx, tr.Content)
			return tr
		} else if len(verdict.Env) > 0 {
			callEnv = append(callEnv, verdict.Env...)
		}
	}

	tr := w.executeToolWithRetry(ctx, call.ID, timeout, retry, func(toolCtx context.Context) agenttools.ToolResult {
		return w.runToolBody(toolCtx, sessionID, projectID, call, toolToAdapter, toolExecutor, scopedCapabilities, callEnv)
	})

	if w.hooks != nil {
		hookCtx.ResultStatus = tr.Status
		hookCtx.ResultStatusSet = true
		hookCtx.ResultExitCode = tr.ExitCode
		hookCtx.ResultExitCodeSet = tr.ExitCodeSet
		tr.Content = w.hooks.RunPost(hookCtx, tr.Content)
	}

	return tr
}

func (w *Worker) runToolBody(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *agenttools.ToolExecutor, scopedCapabilities *capabilities.Registry, callEnv []string) agenttools.ToolResult {
	start := time.Now()
	switch call.Function.Name {
	case agenttools.ToolNameBash, agenttools.ToolNameRead, agenttools.ToolNameWrite:
		raw := toolExecutor.Execute(ctx, call, callEnv...)
		return agenttools.ClassifyBuiltinToolResult(call.ID, call.Function.Name, raw, time.Since(start).Milliseconds())
	case agenttools.ToolNameDelegate:
		raw := w.executeDelegateWithCapabilities(ctx, call, toolExecutor, scopedCapabilities, callEnv)
		return agenttools.ClassifyDelegateRawResult(call.ID, raw, time.Since(start).Milliseconds())
	case agenttools.ToolNameDelegateParallel:
		raw := w.executeDelegateParallel(ctx, call, toolExecutor, scopedCapabilities, callEnv)
		return agenttools.ClassifyDelegateRawResult(call.ID, raw, time.Since(start).Milliseconds())
	default:
		raw := executeCapabilityTool(ctx, call, toolToAdapter, w.capabilities, scopedCapabilities, callEnv)
		return agenttools.ClassifyCapabilityRawResult(call.ID, raw, time.Since(start).Milliseconds())
	}
}

// executeToolWithRetry runs body with a per-attempt timeout and optionally
// retries via RetryingExecutor. Transient retryability is decided inside
// RetryingExecutor.shouldRetry; non-retry paths return classify results unchanged.
func (w *Worker) executeToolWithRetry(
	ctx context.Context,
	callID string,
	timeout time.Duration,
	retry bool,
	body func(toolCtx context.Context) agenttools.ToolResult,
) agenttools.ToolResult {
	runAttempt := func(attemptCtx context.Context) agenttools.ToolResult {
		toolCtx, cancel := context.WithTimeout(attemptCtx, timeout)
		defer cancel()
		tr := body(toolCtx)
		if toolCtx.Err() == context.DeadlineExceeded && attemptCtx.Err() == nil {
			return timeoutToolResult(callID, timeout)
		}
		return tr
	}
	if retry && w.toolRetrier != nil {
		return w.toolRetrier.Execute(ctx, runAttempt)
	}
	return runAttempt(ctx)
}

// executeAgenticTool is a wrapper around DispatchTool for backward compatibility.
// Use DispatchTool directly instead.
func (w *Worker) executeAgenticTool(ctx context.Context, sessionID string, toolExec *agenttools.ToolExecutor, call gateway.ToolCall, toolToAdapter map[string]string) agenttools.ToolResult {
	if toolExec == nil {
		toolExec = w.toolExecutor
	}
	return w.DispatchTool(ctx, sessionID, call, toolToAdapter, toolExec)
}

// executeDelegate handles a delegate tool call from the parent agent.
func (w *Worker) executeDelegate(ctx context.Context, call gateway.ToolCall, toolExecutor *agenttools.ToolExecutor) string {
	return w.executeDelegateWithCapabilities(ctx, call, toolExecutor, nil, nil)
}

func (w *Worker) executeDelegateWithCapabilities(ctx context.Context, call gateway.ToolCall, toolExecutor *agenttools.ToolExecutor, scopedCaps *capabilities.Registry, callEnv []string) string {
	var args delegateArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return agenttools.JSONErrorf("invalid delegate arguments: %v", err)
	}
	if args.Subagent == "" {
		return agenttools.JSONErrorf("subagent name is required")
	}
	if args.Task == "" {
		return agenttools.JSONErrorf("task description is required")
	}

	loader := &agentsubagent.SubagentLoader{}
	def, err := loader.LoadByName(toolExecutor.WorkspacePath(), args.Subagent)
	if err != nil {
		return agenttools.JSONErrorf("failed to load subagent definition: %v", err)
	}

	delegate := agentsubagent.NewSubagentDelegate(
		w.gateway,
		w.sandbox,
		toolExecutor.WorkspacePath(),
		toolExecutor.BuildEnv(callEnv...),
		toolExecutor.WallTimeout(),
		0, // depth=0: parent is delegating
	).WithCapabilities(w.capabilities, scopedCaps).
		WithExternalTools(w.externalTools).
		WithCallEnv(callEnv)

	result, err := delegate.Delegate(
		ctx,
		*def,
		args.Task,
		"", // use default provider
		"", // use default model
		0.2,
		0,
	)
	if err != nil {
		return agenttools.JSONErrorf("delegation failed: %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return agenttools.JSONErrorf("failed to encode subagent result: %v", err)
	}
	return string(encoded)
}

func (w *Worker) executeDelegateParallel(ctx context.Context, call gateway.ToolCall, toolExecutor *agenttools.ToolExecutor, scopedCaps *capabilities.Registry, callEnv []string) string {
	var args delegateParallelArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return agenttools.JSONErrorf("invalid delegate_parallel arguments: %v", err)
	}
	if len(args.Tasks) == 0 {
		return agenttools.JSONErrorf("delegate_parallel requires at least one task")
	}

	loader := &agentsubagent.SubagentLoader{}
	tasks := make([]agentsubagent.ParallelTask, 0, len(args.Tasks))
	for i, task := range args.Tasks {
		if task.Subagent == "" {
			return agenttools.JSONErrorf("task %d subagent name is required", i)
		}
		if task.Task == "" {
			return agenttools.JSONErrorf("task %d description is required", i)
		}
		def, err := loader.LoadByName(toolExecutor.WorkspacePath(), task.Subagent)
		if err != nil {
			return agenttools.JSONErrorf("failed to load subagent definition for task %d: %v", i, err)
		}
		tasks = append(tasks, agentsubagent.ParallelTask{
			Definition:  *def,
			Description: task.Task,
		})
	}

	delegate := agentsubagent.NewSubagentDelegate(
		w.gateway,
		w.sandbox,
		toolExecutor.WorkspacePath(),
		toolExecutor.BuildEnv(callEnv...),
		toolExecutor.WallTimeout(),
		0,
	).WithCapabilities(w.capabilities, scopedCaps).
		WithExternalTools(w.externalTools).
		WithCallEnv(callEnv)

	results := delegate.DelegateParallel(ctx, tasks, "", "", 0.2, 0)
	encoded, err := json.Marshal(results)
	if err != nil {
		return agenttools.JSONErrorf("failed to encode subagent results: %v", err)
	}
	return string(encoded)
}

func toolNamesFromDefinitions(tools []gateway.ToolDefinition) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}
