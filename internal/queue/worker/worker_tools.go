package worker

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
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
// Returns the tool execution result as a string (JSON-encoded for MCP tools, direct for built-in tools).
func (w *Worker) DispatchTool(ctx context.Context, sessionID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor) string {
	return w.dispatchToolWithProject(ctx, sessionID, "", call, toolToAdapter, toolExecutor, nil)
}

func (w *Worker) dispatchToolWithProject(ctx context.Context, sessionID, projectID string, call gateway.ToolCall, toolToAdapter map[string]string, toolExecutor *ToolExecutor, scopedCapabilities *capabilities.Registry) string {
	hookCtx := HookContext{
		ToolName:  call.Function.Name,
		Args:      call.Function.Arguments,
		CallID:    call.ID,
		SessionID: sessionID,
		ProjectID: projectID,
		Timestamp: time.Now(),
		ExecCtx:   ctx,
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
	case toolNameDelegate:
		result = w.executeDelegateWithCapabilities(ctx, call, toolExecutor, scopedCapabilities)
	case toolNameDelegateParallel:
		result = w.executeDelegateParallel(ctx, call, toolExecutor, scopedCapabilities)
	default:
		// Capability tools: never gate on toolToAdapter here; it is only a hint inside
		// executeCapabilityTool. Scoped-then-global resolution (and nil index) is covered
		// by TestDispatchTool_ScopedCapabilityWithoutAdapterIndex.
		result = executeCapabilityTool(ctx, call, toolToAdapter, w.capabilities, scopedCapabilities)
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

// executeDelegate handles a delegate tool call from the parent agent.
func (w *Worker) executeDelegate(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor) string {
	return w.executeDelegateWithCapabilities(ctx, call, toolExecutor, nil)
}

func (w *Worker) executeDelegateWithCapabilities(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor, scopedCaps *capabilities.Registry) string {
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
	def, err := loader.LoadByName(toolExecutor.workspacePath, args.Subagent)
	if err != nil {
		return jsonErrorf("failed to load subagent definition: %v", err)
	}

	delegate := NewSubagentDelegate(
		w.gateway,
		w.sandbox,
		toolExecutor.workspacePath,
		toolExecutor.envVars,
		toolExecutor.wallTimeout,
		0, // depth=0: parent is delegating
	).WithCapabilities(w.capabilities, scopedCaps)

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

func (w *Worker) executeDelegateParallel(ctx context.Context, call gateway.ToolCall, toolExecutor *ToolExecutor, scopedCaps *capabilities.Registry) string {
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
		def, err := loader.LoadByName(toolExecutor.workspacePath, task.Subagent)
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
		toolExecutor.workspacePath,
		toolExecutor.envVars,
		toolExecutor.wallTimeout,
		0,
	).WithCapabilities(w.capabilities, scopedCaps)

	results := delegate.DelegateParallel(ctx, tasks, "", "", 0.2, 0)
	encoded, err := json.Marshal(results)
	if err != nil {
		return jsonErrorf("failed to encode subagent results: %v", err)
	}
	return string(encoded)
}

// executeCapabilityTool routes a capability (MCP) tool call to the correct registry.
//
// It uses resolveCapabilityRoute to check the scoped registry first, then falls back
// to the global registry. This means that if both registries provide the same tool,
// the scoped version wins — the intended behavior for project-scoped plugins.
//
// Historical context: In earlier iterations of processAgenticIteration, the taskCaps
// parameter was declared as _ *capabilities.Registry (unused). Scoped capability
// tools were advertised to the LLM via agenticToolsWithExtras, but they could not
// actually be executed because dispatchToolWithProject always called w.capabilities.CallTool
// directly, bypassing any scoped registry. Now the scoped registry is wired through
// handleAgenticToolCalls → dispatchToolWithHooks → dispatchToolWithProject, so
// scoped tools are both advertised and executable.
func executeCapabilityTool(ctx context.Context, call gateway.ToolCall, toolToAdapter map[string]string, global, scoped *capabilities.Registry) string {
	args, err := parseCapabilityArgs(call.Function.Arguments)
	if err != nil {
		return jsonErrorf("invalid arguments: %v", err)
	}
	adapterName := ""
	if toolToAdapter != nil {
		adapterName = toolToAdapter[call.Function.Name]
	}

	registry, adapterName := resolveCapabilityRoute(ctx, call.Function.Name, adapterName, global, scoped)
	if registry == nil {
		return jsonErrorf("unknown tool: %s", call.Function.Name)
	}
	out, err := registry.CallTool(ctx, adapterName, call.Function.Name, args)
	if err != nil {
		return jsonErrorf("capability tool failed: %v", err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return jsonErrorf("capability tool result encode failed: %v", err)
	}
	return string(encoded)
}

func parseCapabilityArgs(argsJSON string) (map[string]any, error) {
	if strings.TrimSpace(argsJSON) == "" {
		return map[string]any{}, nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return nil, err
	}
	if args == nil {
		args = map[string]any{}
	}
	return args, nil
}

// resolveCapabilityRoute decides which capability registry should handle a tool call.
// The scoped registry is checked first so that project-scoped plugins take priority
// over global ones when both provide the same tool. This was not wired in earlier
// iterations (taskCaps was unused), so scoped tools were advertised to the LLM but
// could not actually be executed.
func resolveCapabilityRoute(ctx context.Context, toolName, adapterHint string, global, scoped *capabilities.Registry) (*capabilities.Registry, string) {
	if registry, adapterName := resolveCapabilityInRegistry(ctx, toolName, adapterHint, scoped); registry != nil {
		return registry, adapterName
	}
	return resolveCapabilityInRegistry(ctx, toolName, adapterHint, global)
}

func resolveCapabilityInRegistry(ctx context.Context, toolName, adapterHint string, registry *capabilities.Registry) (*capabilities.Registry, string) {
	if registry == nil {
		return nil, ""
	}
	if adapterHint != "" {
		if _, ok := registry.GetAdapter(adapterHint); ok {
			return registry, adapterHint
		}
	}
	if adapterName, ok := registry.AdapterForTool(ctx, toolName); ok {
		return registry, adapterName
	}
	return nil, ""
}
