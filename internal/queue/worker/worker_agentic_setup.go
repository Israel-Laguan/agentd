package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	wfilecontext "agentd/internal/agent/filecontext"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (w *Worker) prepareAgenticRun(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
) (
	messages []gateway.PromptMessage,
	tools []gateway.ToolDefinition,
	toolToAdapter map[string]string,
	routingTools []gateway.ToolDefinition,
	routedProfile models.AgentProfile,
	taskToolExecutor *ToolExecutor,
	taskHooks *HookChain,
	taskCaps *capabilities.Registry,
) {
	taskToolExecutor = w.newAgenticTaskToolExecutor(project, task)
	taskHooks, taskCaps = w.mountAgenticHooks(project, profile)
	messages = w.assembleAgenticSystemPrompt(ctx, task, project, profile)
	messages, _ = w.prependReviewRejectionFeedback(ctx, task, messages)
	tools, toolToAdapter = w.agenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)
	routingTools = append([]gateway.ToolDefinition(nil), tools...)
	tools, toolToAdapter = w.filterAgenticTools(tools, toolToAdapter, task, profile)
	routedProfile = w.applyModelRouting(task, profile, messages, routingTools)
	return messages, tools, toolToAdapter, routingTools, routedProfile, taskToolExecutor, taskHooks, taskCaps
}

func (w *Worker) setupAgenticCancel(ctx context.Context, taskID string) (context.Context, func()) {
	cancelCtx, cancel := context.WithCancel(ctx)
	w.registerCancel(taskID, cancel)
	return cancelCtx, func() {
		cancel()
		w.deregisterCancel(taskID)
	}
}

func (w *Worker) newAgenticTaskToolExecutor(project models.Project, task models.Task) *ToolExecutor {
	ex := NewToolExecutor(
		w.sandbox,
		project.WorkspacePath,
		BuildSandboxEnv(w.sandboxEnvAllowlist, w.sandboxExtraEnv),
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

func (w *Worker) runSessionStart(ctx context.Context, task models.Task, project models.Project) error {
	if w.hooks == nil {
		return nil
	}
	return w.hooks.RunSessionStart(HookContext{
		SessionID: task.ID,
		ProjectID: project.ID,
		Timestamp: time.Now(),
		ExecCtx:   ctx,
	})
}

func (w *Worker) mountAgenticHooks(project models.Project, profile models.AgentProfile) (*HookChain, *capabilities.Registry) {
	taskHooks, taskCaps := w.mountScopedPlugins(project, profile)
	if len(profile.GatedTools) > 0 {
		if taskHooks == nil {
			taskHooks = NewHookChain()
		}
		handler := NewBlockingApprovalHandler(w.store)
		taskHooks.RegisterPre(ApprovalGateHook(profile.GatedTools, handler))
	}
	return taskHooks, taskCaps
}

func (w *Worker) newAgenticContextManager(task models.Task) (*ContextManager, *GoalTracker) {
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

	goal := GoalFromTask(task)
	goalTracker := NewGoalTracker(task.ID, task.ProjectID, WithCriteriaStore(w.store))
	if goal != nil {
		goalTracker.SetGoal(*goal)
		cm.SetGoalTracker(goalTracker)
	}
	return cm, goalTracker
}

func (w *Worker) newAgenticContextManagerOnly(task models.Task) *ContextManager {
	cm, _ := w.newAgenticContextManager(task)
	return cm
}

type agenticLoopGuards struct {
	iteration *IterationGuard
	budget    *BudgetGuard
	deadline  *DeadlineGuard
	ctxBudget *ContextBudgetGuard
	cm        *ContextManager
	goals     *GoalTracker
	toolFails *toolFailureTracker
}

func (w *Worker) newAgenticLoopGuards(cancelCtx context.Context, task models.Task) agenticLoopGuards {
	cm, goalTracker := w.newAgenticContextManager(task)
	contextBudget := cm.cfg.AnchorBudget + cm.cfg.WorkingBudget + cm.cfg.CompressedBudget
	return agenticLoopGuards{
		iteration: NewIterationGuard(w.maxToolIterations),
		budget:    NewBudgetGuard(w.budgetTracker, task.ID),
		deadline:  NewDeadlineGuard(cancelCtx),
		ctxBudget: NewContextBudgetGuard(contextBudget, w.contextWarningThreshold),
		cm:        cm,
		goals:     goalTracker,
		toolFails: newToolFailureTracker(w.toolFailureStreak),
	}
}

func encodeCapabilityResult(out any) (string, error) {
	if s, ok := out.(string); ok {
		return s, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("capability result encode: %w", err)
	}
	return string(encoded), nil
}

func (w *Worker) commitCapabilityRouteResult(
	ctx context.Context,
	task models.Task,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
	text string,
) LoopResult {
	if w.messageEditor != nil {
		w.messageEditor.Commit(messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	} else if messages != nil {
		*messages = append(*messages, gateway.PromptMessage{
			Role:    "assistant",
			Content: text,
		})
	}
	w.commitTextWithProfile(ctx, task, text, &profile)
	return LoopResult{Status: LoopSuccessfulCompletion}
}

func (w *Worker) tryExternalCapabilityRoute(
	ctx context.Context,
	task models.Task,
	project models.Project,
	profile models.AgentProfile,
	messages *[]gateway.PromptMessage,
) (LoopResult, bool, error) {
	_ = project
	if w.capabilityRouter == nil {
		return LoopResult{}, false, nil
	}

	decision, ok := w.capabilityRouter.Route(task, profile)
	if !ok {
		return LoopResult{}, false, nil
	}

	if w.capabilities == nil {
		slog.Warn("capability routing: no capability registry; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return LoopResult{}, false, nil
	}

	adapter, found := w.capabilities.GetAdapter(decision.Adapter)
	if !found || adapter == nil {
		slog.Warn("capability routing: adapter not registered; falling through to agentic loop",
			"task_id", task.ID,
			"intent", decision.Intent,
			"adapter", decision.Adapter,
		)
		return LoopResult{}, false, nil
	}

	out, err := w.capabilities.CallTool(ctx, decision.Adapter, decision.Tool, decision.Args)
	if err != nil {
		routeErr := fmt.Errorf("capability routing: %s/%s: %w", decision.Adapter, decision.Tool, err)
		w.failHard(ctx, task, routeErr)
		return LoopResult{}, false, routeErr
	}

	text, err := encodeCapabilityResult(out)
	if err != nil {
		w.failHard(ctx, task, err)
		return LoopResult{}, false, err
	}

	return w.commitCapabilityRouteResult(ctx, task, profile, messages, text), true, nil
}
