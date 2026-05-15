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
// augment the worker-level globals.
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
	sessionID, projectID string,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *ToolExecutor,
	taskHooks *HookChain,
	scopedCapabilities *capabilities.Registry,
) string {
	hookCtx := HookContext{
		ToolName:  call.Function.Name,
		Args:      call.Function.Arguments,
		CallID:    call.ID,
		SessionID: sessionID,
		ProjectID: projectID,
		Timestamp: time.Now(),
	}

	if taskHooks != nil {
		if verdict := taskHooks.RunPre(hookCtx); verdict.ShortCircuit {
			return verdict.Result
		} else if verdict.Veto && verdict.Result != "" {
			result := verdict.Result
			result = taskHooks.RunPost(hookCtx, result)
			return result
		} else if verdict.Veto {
			return jsonErrorf("tool call vetoed by scoped plugin: %s", verdict.Reason)
		}
	}

	result := w.dispatchToolWithProject(ctx, sessionID, projectID, call, toolToAdapter, toolExecutor, scopedCapabilities)

	if taskHooks != nil {
		result = taskHooks.RunPost(hookCtx, result)
	}
	return result
}
