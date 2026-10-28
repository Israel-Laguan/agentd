package agentic

import (
	"context"
	"strings"

	"agentd/internal/api/correlation"

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
	allTools []gateway.ToolDefinition,
	allToolToAdapter map[string]string,
	routedProfile models.AgentProfile,
	taskToolExecutor *agenttools.ToolExecutor,
	taskHooks *agenthooks.HookChain,
	taskCaps *capabilities.Registry,
) {
	taskToolExecutor = e.newAgenticTaskToolExecutor(project, task).
		WithAllowedTools(profile.AllowedTools).
		WithReadOnlyWorkspace(profile.WorkspaceReadonly).
		WithIntent(task.Title + "\n" + task.Description)
	taskHooks, taskCaps = e.host.MountAgenticHooks(project, profile)
	messages = e.buildAgenticMessages(ctx, task, project, profile)
	allTools, allToolToAdapter = e.host.AgenticToolsWithExtras(ctx, taskToolExecutor, taskCaps)
	tools, toolToAdapter, routedProfile = e.selectAgenticTools(task, profile, messages, allTools, allToolToAdapter)
	return messages, tools, toolToAdapter, allTools, allToolToAdapter, routedProfile, taskToolExecutor, taskHooks, taskCaps
}

// selectAgenticTools filters the full tool set for task and routes its model.
// Routing sizes the context by the full set, as the filter only narrows what is
// offered. profile is the unrouted one, so a rerun does not route from a prior
// route.
func (e *Engine) selectAgenticTools(
	task models.Task,
	profile models.AgentProfile,
	messages []gateway.PromptMessage,
	allTools []gateway.ToolDefinition,
	allToolToAdapter map[string]string,
) ([]gateway.ToolDefinition, map[string]string, models.AgentProfile) {
	tools, toolToAdapter := e.host.FilterAgenticTools(allTools, allToolToAdapter, task, profile)
	routed := e.host.ApplyModelRouting(task, profile, messages, allTools)
	return tools, toolToAdapter, routed
}

// buildAgenticMessages is the prompt for task: the layered system prompt and the
// task anchor, with any review-rejection and sandbox-violation feedback.
func (e *Engine) buildAgenticMessages(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) []gateway.PromptMessage {
	messages := e.host.AssembleAgenticSystemPrompt(ctx, task, project, profile)
	messages, _ = e.host.PrependReviewRejectionFeedback(ctx, task, messages)
	return e.host.PrependSandboxViolationFeedback(ctx, task, messages)
}

// runPreTaskElicitation runs pre-task elicitation and reports whether the run
// goes on. Consumed clarification answers extend the description, so the
// evidence guard checks commands against the clarified task; the caller rebuilds
// what was derived from the description before elicitation.
func (e *Engine) runPreTaskElicitation(
	ctx, cancelCtx context.Context,
	task models.Task,
	project models.Project,
	taskToolExecutor *agenttools.ToolExecutor,
) (models.Task, bool) {
	task, blocked, err := e.host.RunPreTaskElicitation(cancelCtx, task, project)
	if err != nil {
		e.host.FailHard(cancelCtx, task, err)
		return task, false
	}
	if blocked {
		correlation.Logger(ctx).DebugContext(ctx, "agentic: pre-task elicitation blocked", "task_id", task.ID)
		return task, false
	}
	correlation.Logger(ctx).DebugContext(ctx, "agentic: pre-task elicitation complete", "task_id", task.ID)
	taskToolExecutor.WithIntent(task.Title + "\n" + task.Description)
	return task, true
}

func (e *Engine) setupAgenticCancel(ctx context.Context, taskID string) (context.Context, func()) {
	e.privilegeAlternativeAttempted = false
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
	).ForTask(task.ID, task.ProjectID)
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
	cm.SetTruncationStrategy(e.config.ContextTruncation)

	goal := agentcontext.GoalFromTask(task)
	goalTracker := agentcontext.NewGoalTracker(
		task.ID,
		task.ProjectID,
		agentcontext.WithCriteriaStore(e.host),
	)
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
