package worker

import (
	"context"
	"log/slog"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

// mountScopedPlugins loads project-scoped and session-scoped plugins,
// returning a task-local HookChain and capabilities Registry that
// augment the worker-level globals. Mount only registers hooks and
// capabilities into the provided chain/registry; it does not mutate
// worker-global state.
func (w *Worker) mountScopedPlugins(
	project models.Project, profile models.AgentProfile,
) (*HookChain, *capabilities.Registry) {
	if w.pluginMounter == nil {
		return nil, nil
	}
	taskHooks := NewHookChain()
	taskCaps := capabilities.NewRegistry()

	if project.WorkspacePath != "" {
		if err := w.pluginMounter.MountProject(project.WorkspacePath, taskHooks, taskCaps); err != nil {
			slog.Warn("failed to load project-scoped plugins",
				"workspace", project.WorkspacePath,
				"error", err,
			)
		}
	}
	if len(profile.Plugins) > 0 {
		if err := w.pluginMounter.MountSession(profile.Plugins, taskHooks, taskCaps); err != nil {
			slog.Warn("failed to load session-scoped plugins",
				"plugins", profile.Plugins,
				"error", err,
			)
		}
	}
	return taskHooks, taskCaps
}

// agenticToolsWithExtras builds the tool definitions and adapter index,
// merging any extra capabilities from scoped plugins.
func (w *Worker) agenticToolsWithExtras(
	ctx context.Context, toolExecutor *ToolExecutor, extra *capabilities.Registry,
) ([]gateway.ToolDefinition, map[string]string) {
	tools, adapterIndex := w.agenticTools(ctx, toolExecutor)
	if extra == nil {
		return tools, adapterIndex
	}
	extraTools, extraIndex, err := extra.GetToolsAndAdapterIndex(ctx)
	if err != nil {
		slog.Warn("failed to get scoped capability tools", "error", err)
		return tools, adapterIndex
	}
	if adapterIndex == nil {
		adapterIndex = make(map[string]string, len(extraIndex))
	}
	for k, v := range extraIndex {
		adapterIndex[k] = v
	}
	return append(tools, extraTools...), adapterIndex
}

// dispatchToolWithHooks wraps dispatchToolWithProject and additionally
// runs task-scoped plugin hooks (pre and post) around the call.
func (w *Worker) dispatchToolWithHooks(
	ctx context.Context,
	sessionID, projectID, turnID string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *ToolExecutor,
	taskHooks *HookChain,
	scopedCapabilities *capabilities.Registry,
) (ToolResult, bool) {
	var verdicts []string
	hookCtx := HookContext{
		ToolName:      call.Function.Name,
		Args:          call.Function.Arguments,
		CallID:        call.ID,
		SessionID:     sessionID,
		ProjectID:     projectID,
		TurnID:        turnID,
		Timestamp:     time.Now(),
		TaskUpdatedAt: taskUpdatedAt,
		ExecCtx:       ctx,
		Verdicts:      &verdicts,
	}
	if w.budgetTracker != nil && sessionID != "" {
		hookCtx.TokenCountBefore = w.budgetTracker.Usage(sessionID)
	}

	tr, suspended, callEnv, handled := w.applyTaskPreHooks(hookCtx, call, taskHooks)
	if handled {
		return tr, suspended
	}

	retry := w.toolRetrier != nil && w.toolRetries.Allows(call.Function.Name)
	tr = w.dispatchToolWithProject(ctx, sessionID, projectID, call, toolToAdapter, toolExecutor, scopedCapabilities, retry, callEnv, &hookCtx)

	if taskHooks != nil {
		hookCtx.ResultStatus = tr.Status
		hookCtx.ResultStatusSet = true
		hookCtx.ResultExitCode = tr.ExitCode
		hookCtx.ResultExitCodeSet = tr.ExitCodeSet
		tr.Content = taskHooks.RunPost(hookCtx, tr.Content)
	}
	w.finalizeDispatchAudit(hookCtx, tr)
	return tr, false
}

// applyTaskPreHooks runs task-scoped pre-hooks. When handled is true, tr and suspended are final.
func (w *Worker) applyTaskPreHooks(
	hookCtx HookContext,
	call gateway.ToolCall,
	taskHooks *HookChain,
) (tr ToolResult, suspended bool, callEnv []string, handled bool) {
	if taskHooks == nil {
		return ToolResult{}, false, nil, false
	}
	verdict := taskHooks.RunPre(hookCtx)
	if verdict.ShortCircuit {
		tr = classifyPrecomputedToolResult(call.ID, call.Function.Name, verdict.Result, 0)
		tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
		return tr, verdict.Suspend, nil, true
	}
	if verdict.Veto && verdict.Result != "" {
		// Suspend controls agentic loop pause and status: substitute answers
		// continue as Success; human-review gates surface as Vetoed.
		if verdict.Suspend {
			tr = VetoedResult(call.ID, verdict.Result)
			tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
			return tr, true, nil, true
		}
		tr = SuccessResult(call.ID, verdict.Result, 0)
		tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
		return tr, false, nil, true
	}
	if verdict.Veto {
		tr = VetoedResult(call.ID, verdict.Reason)
		tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
		return tr, verdict.Suspend, nil, true
	}
	if len(verdict.Env) > 0 {
		callEnv = append(callEnv, verdict.Env...)
	}
	return ToolResult{}, false, callEnv, false
}

// runDispatchPostHooks runs worker-level then task-scoped post-hooks.
// Used when task pre-hooks veto before executeToolCore (which would normally
// run worker post-hooks including audit).
func (w *Worker) runDispatchPostHooks(hookCtx HookContext, tr ToolResult, taskHooks *HookChain) string {
	hookCtx.ResultStatus = tr.Status
	hookCtx.ResultStatusSet = true
	hookCtx.ResultExitCode = tr.ExitCode
	hookCtx.ResultExitCodeSet = tr.ExitCodeSet
	content := tr.Content
	if w.hooks != nil {
		content = w.hooks.RunPost(hookCtx, content)
	}
	if taskHooks != nil {
		content = taskHooks.RunPost(hookCtx, content)
	}
	w.finalizeDispatchAudit(hookCtx, tr)
	return content
}

func (w *Worker) finalizeDispatchAudit(hookCtx HookContext, tr ToolResult) {
	if w.budgetTracker != nil && hookCtx.SessionID != "" {
		hookCtx.TokenCountAfter = w.budgetTracker.Usage(hookCtx.SessionID)
	}
	var verdicts []string
	if hookCtx.Verdicts != nil {
		verdicts = *hookCtx.Verdicts
	}
	w.recordToolDispatch(hookCtx, tr, verdicts)
}
