package worker

import (
	"context"
	"log/slog"
	"strings"
	"time"

	wfilecontext "agentd/internal/agent/filecontext"
	agenthooks "agentd/internal/agent/hooks"
	wsession "agentd/internal/agent/session"
	agentsubagent "agentd/internal/agent/subagent"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

func buildWorkerHooks(
	opts WorkerOptions,
	toolExecutor *agenttools.ToolExecutor,
	sink models.EventSink,
	scrubber sandbox.Scrubber,
) *agenthooks.HookChain {
	base := opts.Hooks
	if base == nil {
		base = agenthooks.NewHookChain()
	}
	hooks := base.Clone()
	hooks.RegisterPre(agenthooks.SchemaValidationHook(agenttools.SchemaRegistryFromDefinitions(toolExecutor.Definitions())))
	if !opts.DisableCredentialDetection {
		hooks.RegisterPre(agenthooks.CredentialDetectionHook())
	}
	if len(opts.ToolCredentials) > 0 {
		store := wsession.NewEnvSecretStore(opts.ToolCredentials)
		hooks.RegisterPre(agenthooks.CredentialInjectionHook(store))
		hooks.RegisterSessionStart(agenthooks.CredentialValidationSessionHook(store))
	}
	hooks.PrependPost(agenthooks.ScrubResultHook(scrubber))
	hooks.RegisterPost(agenthooks.InjectionResistanceHook(agenthooks.ExternalToolsSet(opts.ExternalTools)))
	hooks.RegisterPost(agenthooks.AuditHook(sink, scrubber))
	return hooks
}

// MountAgenticHooks loads project-scoped and session-scoped plugins (when a
// PluginMounter is configured) and registers ApprovalGateHook for any
// GatedTools in the profile. It returns a task-local HookChain and
// capabilities Registry that augment worker-level globals. It only
// registers into the returned values; it does not mutate worker-global state.
// Returns nil, nil when there is no mounter and no gated tools.
func (w *Worker) MountAgenticHooks(
	project models.Project, profile models.AgentProfile,
) (*agenthooks.HookChain, *capabilities.Registry) {
	var taskHooks *agenthooks.HookChain
	var taskCaps *capabilities.Registry

	if len(profile.GatedTools) > 0 {
		taskHooks = agenthooks.NewHookChain()
		handler := NewBlockingApprovalHandler(w.store)
		taskHooks.RegisterPre(ApprovalGateHook(profile.GatedTools, handler))
	}

	if w.pluginMounter == nil {
		return taskHooks, taskCaps
	}

	if taskHooks == nil {
		taskHooks = agenthooks.NewHookChain()
	}
	if taskCaps == nil {
		taskCaps = capabilities.NewRegistry()
	}

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

// RunSessionStart runs the worker's SessionStart hooks (e.g. credential
// validation) followed by any task-scoped hooks supplied by MountAgenticHooks.
func (w *Worker) RunSessionStart(ctx context.Context, task models.Task, project models.Project, taskHooks *agenthooks.HookChain) error {
	// Run worker-level hooks first.
	if w.hooks != nil {
		if err := w.hooks.RunSessionStart(agenthooks.HookContext{
			SessionID: task.ID,
			ProjectID: project.ID,
			Timestamp: time.Now(),
			ExecCtx:   ctx,
		}); err != nil {
			return err
		}
	}
	// Then run task-scoped hooks from scoped plugins.
	if taskHooks != nil {
		return taskHooks.RunSessionStart(agenthooks.HookContext{
			SessionID: task.ID,
			ProjectID: project.ID,
			Timestamp: time.Now(),
			ExecCtx:   ctx,
		})
	}
	return nil
}

// AgenticToolsWithExtras builds the tool definitions and adapter index,
// merging any extra capabilities from scoped plugins (from MountAgenticHooks).
func (w *Worker) AgenticToolsWithExtras(
	ctx context.Context, toolExecutor *agenttools.ToolExecutor, extra *capabilities.Registry,
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
	tools = append(tools, extraTools...)
	gateway.SortTools(tools)
	return tools, adapterIndex
}

// DispatchToolWithHooks wraps dispatchToolWithProject and additionally
// runs task-scoped plugin hooks (pre and post) around the call.
func (w *Worker) DispatchToolWithHooks(
	ctx context.Context,
	sessionID, projectID, turnID string,
	taskUpdatedAt time.Time,
	call gateway.ToolCall,
	toolToAdapter map[string]string,
	toolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	scopedCapabilities *capabilities.Registry,
	providerName string,
) (agenttools.ToolResult, bool) {

	var verdicts []string
	hookCtx := agenthooks.HookContext{
		ToolName:      call.Function.Name,
		Args:          call.Function.Arguments,
		CallID:        call.ID,
		SessionID:     sessionID,
		ProjectID:     projectID,
		Provider:      providerName,
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
	hookCtx agenthooks.HookContext,
	call gateway.ToolCall,
	taskHooks *agenthooks.HookChain,
) (tr agenttools.ToolResult, suspended bool, callEnv []string, handled bool) {
	if taskHooks == nil {
		return agenttools.ToolResult{}, false, nil, false
	}
	verdict := taskHooks.RunPre(hookCtx)
	if verdict.ShortCircuit {
		tr = agenttools.ClassifyPrecomputedToolResult(call.ID, call.Function.Name, verdict.Result, 0)
		tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
		return tr, verdict.Suspend, nil, true
	}
	if verdict.Veto && verdict.Result != "" {
		// Suspend controls agentic loop pause and status: substitute answers
		// continue as Success; human-review gates surface as Vetoed.
		if verdict.Suspend {
			tr = agenttools.VetoedResult(call.ID, verdict.Result)
			tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
			return tr, true, nil, true
		}
		tr = agenttools.SuccessResult(call.ID, verdict.Result, 0)
		tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
		return tr, false, nil, true
	}
	if verdict.Veto {
		tr = agenttools.VetoedResult(call.ID, verdict.Reason)
		tr.Content = w.runDispatchPostHooks(hookCtx, tr, taskHooks)
		return tr, verdict.Suspend, nil, true
	}
	if len(verdict.Env) > 0 {
		callEnv = append(callEnv, verdict.Env...)
	}
	return agenttools.ToolResult{}, false, callEnv, false
}

// runDispatchPostHooks runs worker-level then task-scoped post-hooks.
// Used when task pre-hooks veto before executeToolCore (which would normally
// run worker post-hooks including audit).
func (w *Worker) runDispatchPostHooks(hookCtx agenthooks.HookContext, tr agenttools.ToolResult, taskHooks *agenthooks.HookChain) string {
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

func (w *Worker) newAgenticTaskToolExecutor(project models.Project, task models.Task) *agenttools.ToolExecutor {
	ex := agenttools.NewToolExecutor(
		w.sandbox,
		project.WorkspacePath,
		agenttools.BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
		w.sandboxWallTimeout,
	)
	if w.fileContextCfg.Enabled && w.docStore != nil {
		taskQuery := strings.TrimSpace(task.Description)
		if ctx := strings.TrimSpace(project.OriginalInput); ctx != "" {
			if taskQuery != "" {
				taskQuery += "\n"
			}
			taskQuery += ctx
		}
		var embedder wfilecontext.Embedder
		if w.gateway != nil {
			embedder = &wfilecontext.GatewayEmbedder{
				Gateway: w.gateway,
				Model:   w.fileContextCfg.EmbeddingModel,
			}
		}
		ex.SetFilePipeline(wfilecontext.NewFilePipeline(wfilecontext.FilePipelineConfig{
			Workspace: project.WorkspacePath,
			Store:     w.docStore,
			Embedder:  embedder,
			TopK:      w.fileContextCfg.TopK,
			TaskQuery: taskQuery,
			Pinned:    wfilecontext.ParsePinnedPaths(taskQuery),
		}))
	}
	return ex
}

func (w *Worker) agenticTools(ctx context.Context, toolExecutor *agenttools.ToolExecutor) ([]gateway.ToolDefinition, map[string]string) {
	tools := append([]gateway.ToolDefinition(nil), toolExecutor.Definitions()...)
	tools = append(tools, agentsubagent.DelegateToolDefinition(), agentsubagent.DelegateParallelToolDefinition())
	if w.capabilities == nil {
		gateway.SortTools(tools)
		return tools, nil
	}
	capabilityTools, toolToAdapter, err := w.capabilities.GetToolsAndAdapterIndex(ctx)
	if err != nil {
		slog.Warn("failed to get capability tools", "error", err)
		gateway.SortTools(tools)
		return tools, nil
	}
	tools = append(tools, capabilityTools...)
	gateway.SortTools(tools)
	return tools, toolToAdapter
}
