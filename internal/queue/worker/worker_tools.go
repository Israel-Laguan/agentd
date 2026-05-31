package worker

import (
	"context"
	"encoding/json"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)


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
func (w *Worker) DispatchTool(ctx context.Context, sessionID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor) ToolResult {
	retry := w.toolRetrier != nil && w.toolRetries.Allows(call.Function.Name)
	return w.dispatchToolWithProject(ctx, sessionID, "", call, toolToAdapter, toolExecutor, nil, retry, nil, nil)
}

// timeoutToolResult returns a structured ToolResult for a timed-out tool.
func timeoutToolResult(callID string, timeout time.Duration) ToolResult {
	return TimeoutResult(callID, timeout.Milliseconds())
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

func (w *Worker) dispatchToolWithProject(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor, scopedCapabilities *capabilities.Registry, retry bool, callEnv []string, auditParent *HookContext) ToolResult {
	timeout := w.toolTimeouts.Lookup(call.Function.Name, config.DefaultToolTimeout)
	return w.executeToolCore(ctx, sessionID, projectID, call, toolToAdapter, toolExecutor, scopedCapabilities, timeout, retry, callEnv, auditParent)
}

func (w *Worker) executeToolCore(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor, scopedCapabilities *capabilities.Registry, timeout time.Duration, retry bool, callEnv []string, auditParent *HookContext) ToolResult {
	start := time.Now()
	hookCtx := HookContext{
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
			return classifyPrecomputedToolResult(call.ID, call.Function.Name, verdict.Result, time.Since(start).Milliseconds())
		} else if verdict.Veto && verdict.Result != "" {
			result := verdict.Result
			tr := SuccessResult(call.ID, result, time.Since(start).Milliseconds())
			hookCtx.ResultStatus = tr.Status
			hookCtx.ResultStatusSet = true
			tr.Content = w.hooks.RunPost(hookCtx, tr.Content)
			return tr
		} else if verdict.Veto {
			tr := VetoedResult(call.ID, verdict.Reason)
			hookCtx.ResultStatus = tr.Status
			hookCtx.ResultStatusSet = true
			tr.Content = w.hooks.RunPost(hookCtx, tr.Content)
			return tr
		} else if len(verdict.Env) > 0 {
			callEnv = append(callEnv, verdict.Env...)
		}
	}

	tr := w.executeToolWithRetry(ctx, call.ID, timeout, retry, func(toolCtx context.Context) ToolResult {
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

func (w *Worker) runToolBody(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor, scopedCapabilities *capabilities.Registry, callEnv []string) ToolResult {
	start := time.Now()
	switch call.Function.Name {
	case toolNameBash, toolNameRead, toolNameWrite:
		raw := toolExecutor.Execute(ctx, call, callEnv...)
		return classifyBuiltinToolResult(call.ID, call.Function.Name, raw, time.Since(start).Milliseconds())
	case toolNameDelegate:
		raw := w.executeDelegateWithCapabilities(ctx, call, toolExecutor, scopedCapabilities, callEnv)
		return classifyDelegateRawResult(call.ID, raw, time.Since(start).Milliseconds())
	case toolNameDelegateParallel:
		raw := w.executeDelegateParallel(ctx, call, toolExecutor, scopedCapabilities, callEnv)
		return classifyDelegateRawResult(call.ID, raw, time.Since(start).Milliseconds())
	default:
		raw := executeCapabilityTool(ctx, call, toolToAdapter, w.capabilities, scopedCapabilities, callEnv)
		return classifyCapabilityRawResult(call.ID, raw, time.Since(start).Milliseconds())
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
	body func(toolCtx context.Context) ToolResult,
) ToolResult {
	runAttempt := func(attemptCtx context.Context) ToolResult {
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
func (w *Worker) executeAgenticTool(ctx context.Context, sessionID string, toolExec *ToolExecutor, call gateway.ToolCall, toolToAdapter map[string]string) ToolResult {
	if toolExec == nil {
		toolExec = w.toolExecutor
	}
	return w.DispatchTool(ctx, sessionID, call, toolToAdapter, toolExec)
}

// executeDelegate handles a delegate tool call from the parent agent.
func (w *Worker) executeDelegate(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor) string {
	return w.executeDelegateWithCapabilities(ctx, call, toolExecutor, nil, nil)
}

func (w *Worker) executeDelegateWithCapabilities(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor, scopedCaps *capabilities.Registry, callEnv []string) string {
	var args delegateArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return jsonErrorf("invalid delegate arguments: %v", err)
	}
	if args.Subagent == "" {
		return jsonErrorf("subagent name is required")
	}
	if args.Task == "" {
		return jsonErrorf("task description is required")
	}

	loader := &SubagentLoader{}
	def, err := loader.LoadByName(toolExecutor.WorkspacePath(), args.Subagent)
	if err != nil {
		return jsonErrorf("failed to load subagent definition: %v", err)
	}

	delegate := NewSubagentDelegate(
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
		return jsonErrorf("delegation failed: %v", err)
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return jsonErrorf("failed to encode subagent result: %v", err)
	}
	return string(encoded)
}

func (w *Worker) executeDelegateParallel(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor, scopedCaps *capabilities.Registry, callEnv []string) string {
	var args delegateParallelArgs
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return jsonErrorf("invalid delegate_parallel arguments: %v", err)
	}
	if len(args.Tasks) == 0 {
		return jsonErrorf("delegate_parallel requires at least one task")
	}

	loader := &SubagentLoader{}
	tasks := make([]ParallelTask, 0, len(args.Tasks))
	for i, task := range args.Tasks {
		if task.Subagent == "" {
			return jsonErrorf("task %d subagent name is required", i)
		}
		if task.Task == "" {
			return jsonErrorf("task %d description is required", i)
		}
		def, err := loader.LoadByName(toolExecutor.WorkspacePath(), task.Subagent)
		if err != nil {
			return jsonErrorf("failed to load subagent definition for task %d: %v", i, err)
		}
		tasks = append(tasks, ParallelTask{
			Definition:  *def,
			Description: task.Task,
		})
	}

	delegate := NewSubagentDelegate(
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
		return jsonErrorf("failed to encode subagent results: %v", err)
	}
	return string(encoded)
}


