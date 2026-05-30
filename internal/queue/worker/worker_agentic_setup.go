package worker

import (
	"context"
	"strings"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"

	wfilecontext "agentd/internal/queue/worker/filecontext"
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
		ex.filePipeline = wfilecontext.NewFilePipeline(wfilecontext.FilePipelineConfig{
			Workspace: project.WorkspacePath,
			Store:     w.docStore,
			Embedder:  embedder,
			TopK:      w.fileContextCfg.TopK,
			TaskQuery: taskQuery,
			Pinned:    wfilecontext.ParsePinnedPaths(taskQuery),
		})
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
