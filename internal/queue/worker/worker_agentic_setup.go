package worker

import (
	"context"
	"strings"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/models"
)

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
		var embedder Embedder
		if w.gateway != nil {
			embedder = &GatewayEmbedder{
				Gateway: w.gateway,
				Model:   w.fileContextCfg.EmbeddingModel,
			}
		}
		ex.filePipeline = NewFilePipeline(FilePipelineConfig{
			Workspace: project.WorkspacePath,
			Store:     w.docStore,
			Embedder:  embedder,
			TopK:      w.fileContextCfg.TopK,
			TaskQuery: taskQuery,
			Pinned:    ParsePinnedPaths(taskQuery),
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
	goalTracker := NewGoalTracker(task.ID, task.ProjectID)
	if goal != nil {
		goalTracker.SetGoal(*goal)
		cm.SetGoalTracker(goalTracker)
	}
	return cm, goalTracker
}
