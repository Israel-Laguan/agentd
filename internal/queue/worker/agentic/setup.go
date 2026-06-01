package agentic

import (
	"context"
	"strings"

	agentcontext "agentd/internal/agent/context"
	wfilecontext "agentd/internal/agent/filecontext"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func (e *Engine) prepareAgenticRun(
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
	taskToolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	taskCaps *capabilities.Registry,
) {
	taskToolExecutor = e.newAgenticTaskToolExecutor(project, task)
	taskHooks, taskCaps = e.host.MountAgenticHooks(project, profile)
	messages = e.host.AssembleAgenticSystemPrompt(ctx, task, project, profile)
	messages, _ = e.host.PrependReviewRejectionFeedback(ctx, task, messages)
	tools, toolToAdapter = e.host.AgenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)
	routingTools = append([]gateway.ToolDefinition(nil), tools...)
	tools, toolToAdapter = e.host.FilterAgenticTools(tools, toolToAdapter, task, profile)
	routedProfile = e.host.ApplyModelRouting(task, profile, messages, routingTools)
	return messages, tools, toolToAdapter, routingTools, routedProfile, taskToolExecutor, taskHooks, taskCaps
}

func (e *Engine) setupAgenticCancel(ctx context.Context, taskID string) (context.Context, func()) {
	cancelCtx, cancel := context.WithCancel(ctx)
	e.host.RegisterCancel(taskID, cancel)
	return cancelCtx, func() {
		cancel()
		e.host.DeregisterCancel(taskID)
	}
}

func (e *Engine) newAgenticTaskToolExecutor(project models.Project, task models.Task) *agenttools.ToolExecutor {
	ex := agenttools.NewToolExecutor(
		e.config.Sandbox,
		project.WorkspacePath, agenttools.BuildSandboxEnv(e.config.SandboxEnvAllowlist, e.config.SandboxExtraEnv), e.config.SandboxWallTimeout,
	)
	if e.config.FileContextCfg.Enabled && e.config.DocStore != nil {
		taskQuery := strings.TrimSpace(task.Description)
		if ctx := strings.TrimSpace(project.OriginalInput); ctx != "" {
			if taskQuery != "" {
				taskQuery += "\n"
			}
			taskQuery += ctx
		}
		var embedder wfilecontext.Embedder
		if e.config.Gateway != nil {
			embedder = &wfilecontext.GatewayEmbedder{
				Gateway: e.config.Gateway,
				Model:   e.config.FileContextCfg.EmbeddingModel,
			}
		}
		ex.SetFilePipeline(wfilecontext.NewFilePipeline(wfilecontext.FilePipelineConfig{
			Workspace: project.WorkspacePath,
			Store:     e.config.DocStore,
			Embedder:  embedder,
			TopK:      e.config.FileContextCfg.TopK,
			TaskQuery: taskQuery,
			Pinned:    wfilecontext.ParsePinnedPaths(taskQuery),
		}))
	}
	return ex
}

func (e *Engine) newAgenticContextManager(task models.Task) (*agentcontext.ContextManager, *agentcontext.GoalTracker) {
	contextCfg := e.config.ContextCfg
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

	cm := agentcontext.NewContextManager(
		contextCfg,
		e.config.Gateway,
		task.AgentID,
		task.ID,
	)

	goal := agentcontext.GoalFromTask(task)
	goalTracker := agentcontext.NewGoalTracker(task.ID, task.ProjectID, agentcontext.WithCriteriaStore(e.config.Store))
	if goal != nil {
		goalTracker.SetGoal(*goal)
		cm.SetGoalTracker(goalTracker)
	}
	return cm, goalTracker
}

func (e *Engine) newAgenticContextManagerOnly(task models.Task) *agentcontext.ContextManager {
	cm, _ := e.newAgenticContextManager(task)
	return cm
}

type agenticLoopGuards struct {
	iteration *agentruntime.IterationGuard
	budget    *agentruntime.BudgetGuard
	deadline  *agentruntime.DeadlineGuard
	ctxBudget *agentruntime.ContextBudgetGuard
	cm        *agentcontext.ContextManager
	goals     *agentcontext.GoalTracker
	toolFails *agenttools.ToolFailureTracker
}

func (e *Engine) newAgenticLoopGuards(cancelCtx context.Context, task models.Task) agenticLoopGuards {
	cm, goalTracker := e.newAgenticContextManager(task)
	contextBudget := cm.TotalBudget()
	return agenticLoopGuards{
		iteration: agentruntime.NewIterationGuard(e.config.MaxToolIterations),
		budget:    agentruntime.NewBudgetGuard(e.config.BudgetTracker, task.ID),
		deadline:  agentruntime.NewDeadlineGuard(cancelCtx),
		ctxBudget: agentruntime.NewContextBudgetGuard(contextBudget, e.config.ContextWarningThreshold),
		cm:        cm,
		goals:     goalTracker,
		toolFails: agenttools.NewToolFailureTracker(e.config.ToolFailureStreak),
	}
}
