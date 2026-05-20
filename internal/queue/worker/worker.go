package worker

import (
	"context"
	"log/slog"
	"os"
	"time"

	"agentd/internal/capabilities"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
)

// DefaultMaxRetries is the baseline retry budget before eviction.
const DefaultMaxRetries = 3

type Worker struct {
	store                models.KanbanStore
	gateway              gateway.AIGateway
	sandbox              sandbox.Executor
	breaker              *safety.CircuitBreaker
	sink                 models.EventSink
	canceller            *CancelRegistry
	tuner                *planning.ParameterTuner
	retriever            MemoryRetriever
	heartbeatInterval    time.Duration
	sandboxWallTimeout   time.Duration
	sandboxEnvAllowlist  []string
	sandboxExtraEnv      []string
	sandboxScrubber      sandbox.Scrubber
	maxRetries           int
	maxToolIterations    int
	truncatorMax         int
	characterBudget      int
	toolExecutor         *ToolExecutor
	toolTimeouts         config.ToolTimeoutsConfig
	toolRetries          config.ToolRetriesConfig
	toolRetrier          *RetryingExecutor
	capabilities         *capabilities.Registry
	tokenBudget          int
	budgetTracker        spec.BudgetTracker
	hooks                *HookChain
	pluginMounter        PluginMounter
	contextCfg           config.AgenticContextConfig
	instructionLoader    *InstructionLoader
	skillLoader          *SkillLoader
	skillRouter          *SkillRouter
	legacyHandoffTimeout time.Duration
	externalTools             map[string]struct{}
	auditLogger               *AuditLogger
	contextWarningThreshold   float64
	toolFailureStreak         int
	tokenUsageHook            func(int)
	loopResultRecorder        func(LoopResult)
	fileContextCfg            config.FileContextConfig
	docStore                  *DocStore
	planningCfg               config.AgenticPlanningConfig
	messageEditor             *MessageEditor
	checkpointStore           CheckpointStore
}

// MemoryRetriever is an optional dependency for pre-fetching durable memories.
type MemoryRetriever interface {
	Recall(ctx context.Context, intent, projectID, userID string) []models.Memory
}

// PluginMounter loads and mounts plugins from a directory into a
// HookChain and capabilities Registry. The worker calls this to
// mount project-scoped plugins (from workspace directories) and
// session-scoped plugins (by name from AgentProfile.Plugins).
type PluginMounter interface {
	MountProject(workspacePath string, chain *HookChain, registry *capabilities.Registry) error
	MountSession(names []string, chain *HookChain, registry *capabilities.Registry) error
}

// Process handles task execution, supporting two modes:
// - Legacy mode (default): single-shot JSON command execution via GenerateJSON
// - Agentic mode: inner loop with tool calling and message accumulation (processAgentic)
// Routing is determined by profile.AgenticMode flag. When agentic mode is enabled but
// the provider doesn't support tool round-tripping, falls back to legacy mode.
func (w *Worker) Process(ctx context.Context, task models.Task) {
	defer w.recoverPanic(ctx, task)
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		w.failHard(ctx, task, err)
		return
	}
	running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, os.Getpid())
	if err != nil {
		return
	}
	task = *running
	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, w.store))
	stopHeartbeat := w.startHeartbeat(ctx, task.ID)
	defer stopHeartbeat()
	if profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.failHard(ctx, task, err)
			return
		} else if done {
			return
		}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, *project)
		return
	}
	// Routing: check AgenticMode flag to determine execution path
	// Agentic mode requires provider support (see agenticProviders)
	if profile.AgenticMode {
		if w.providerSupportsAgentic(*profile) {
			if result, ok := w.processAgentic(ctx, task, *project, *profile); ok {
				w.handleLoopResult(ctx, task, result)
			}
			return
		}
		slog.Warn("agentic mode requested but provider does not support tool round-tripping; falling back to legacy mode",
			"task_id", task.ID,
			"provider", profile.Provider,
		)
	}
	w.runLegacyTask(ctx, task, *project, *profile)
}

func (w *Worker) runLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) {
	response, err := w.command(ctx, task, profile)
	if err != nil {
		w.handleGatewayError(ctx, task, err)
		return
	}
	if response.TooComplex {
		w.handleTaskBreakdown(ctx, task, response.Subtasks)
		return
	}
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	w.registerCancel(task.ID, cancel)
	defer w.deregisterCancel(task.ID)
	command := response.Command
	result, runErr := w.sandbox.Execute(execCtx, w.payload(task, project, command))
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, project, command, result)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, command, result)
		return
	}
	if profile.RequireReview && runErr == nil && result.Success {
		w.createReviewHandoff(ctx, task, result.Stdout)
		return
	}
	w.commit(ctx, task, result, runErr)
}
