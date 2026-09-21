package worker

import (
	"context"
	"encoding/json"
	"fmt"
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

	agentcontext "agentd/internal/agent/context"
	wfilecontext "agentd/internal/agent/filecontext"
	agenthooks "agentd/internal/agent/hooks"
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	wskills "agentd/internal/agent/skills"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/queue/worker/agentic"
)

// DefaultMaxRetries is the baseline retry budget before eviction.
const DefaultMaxRetries = 3

type Worker struct {
	store                         models.KanbanStore
	gateway                       gateway.AIGateway
	sandbox                       sandbox.Executor
	breaker                       *safety.CircuitBreaker
	providerBreakers              *safety.ProviderBreakers
	sink                          models.EventSink
	canceller                     *CancelRegistry
	tuner                         *planning.ParameterTuner
	retriever                     MemoryRetriever
	heartbeatInterval             time.Duration
	sandboxWallTimeout            time.Duration
	sandboxEnvAllowlist           []string
	sandboxExtraEnv               []string
	sandboxScrubber               sandbox.Scrubber
	maxRetries                    int
	maxToolIterations             int
	truncatorMax                  int
	characterBudget               int
	toolExecutor                  *agenttools.ToolExecutor
	toolTimeouts                  config.ToolTimeoutsConfig
	toolRetries                   config.ToolRetriesConfig
	toolRetrier                   *agenttools.RetryingExecutor
	capabilities                  *capabilities.Registry
	tokenBudget                   int
	budgetTracker                 spec.BudgetTracker
	hooks                         *agenthooks.HookChain
	pluginMounter                 PluginMounter
	contextCfg                    config.AgenticContextConfig
	instructionLoader             *InstructionLoader
	skillLoader                   *wskills.SkillLoader
	skillRouter                   *wskills.SkillRouter
	legacyHandoffTimeout          time.Duration
	externalTools                 map[string]struct{}
	auditLogger                   *agentruntime.AuditLogger
	contextWarningThreshold       float64
	toolFailureStreak             int
	tokenUsageHook                func(int)
	tokenStore                    TokenUsageStore
	disableTokenRecording         bool
	loopResultRecorder            func(LoopResult)
	fileContextCfg                config.FileContextConfig
	docStore                      *wfilecontext.DocStore
	planningCfg                   config.AgenticPlanningConfig
	tieredCfg                     config.TieredConfig
	messageEditor                 *agentcontext.MessageEditor
	checkpointStore               wsession.CheckpointStore
	topicGuard                    *agentruntime.TopicGuard
	modelRouter                   *agentruntime.ModelRouter
	toolManifest                  *agenttools.ToolManifest
	capabilityRouter              *agentruntime.CapabilityRouter
	batcher                       *TaskBatcher
	promptLibrary                 *agentruntime.PromptLibrary
	healingEnabled                bool
	maxHealingTasks               int
	legacyMaxBreakdownDepth       int
	legacyMaxSubtasksPerBreakdown int
	legacyPreflightScore          int
	legacyRejectScore             int
	legacyMaxDescriptionLen       int
}

type MemoryRetriever interface {
	Recall(ctx context.Context, intent, projectID, userID string) []models.Memory
}

type TokenUsageStore interface {
	AddTokenUsage(ctx context.Context, taskID string, tokens int) error
	AddUsageDetails(ctx context.Context, taskID string, details spec.UsageDetails) error
}

type PluginMounter interface {
	MountProject(workspacePath string, chain *agenthooks.HookChain, registry *capabilities.Registry) error
	MountSession(names []string, chain *agenthooks.HookChain, registry *capabilities.Registry) error
}

func (w *Worker) RecordTaskTokenUsage(ctx context.Context, task models.Task, tokens int, details spec.UsageDetails) {
	if tokens <= 0 {
		return
	}
	if w.tokenUsageHook != nil {
		w.tokenUsageHook(tokens)
	}
	if w.tokenStore != nil && !w.disableTokenRecording {
		if err := w.tokenStore.AddTokenUsage(ctx, task.ID, tokens); err != nil {
			slog.Error("failed to persist token usage", "task_id", task.ID, "tokens", tokens, "err", err)
		}
		if err := w.tokenStore.AddUsageDetails(ctx, task.ID, details); err != nil {
			slog.Error("failed to persist usage details", "task_id", task.ID, "err", err)
		}
	}
	slog.Debug("recorded token usage", "task_id", task.ID, "tokens", tokens, "cached_tokens", details.CachedTokens, "cache_write_tokens", details.CacheWriteTokens)
	payload, _ := json.Marshal(models.TokenUsagePayload{Tokens: tokens, CachedTokens: details.CachedTokens, CacheWriteTokens: details.CacheWriteTokens})
	w.Emit(ctx, task, string(models.EventTypeTokenUsage), string(payload))
}

// Process handles task execution: legacy (GenerateJSON), agentic (tool calling),
// tiered dispatch, and review finalization. Model routing runs once per path.
func (w *Worker) Process(ctx context.Context, task models.Task) {
	defer w.recoverPanic(ctx, task)
	slog.DebugContext(ctx, "worker: processing task",
		"task_id", task.ID, "agent_id", task.AgentID, "state", task.State)
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		slog.ErrorContext(ctx, "worker: failed to load context",
			"task_id", task.ID, "error", err)
		w.FailHard(ctx, task, err)
		return
	}
	w.warnIfWorkspaceEmpty(ctx, task, project)
	running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, os.Getpid())
	if err != nil {
		slog.DebugContext(ctx, "worker: failed to mark running (concurrent claim?)",
			"task_id", task.ID, "error", err)
		return
	}
	task = *running
	ctx = gateway.WithHouseRules(ctx, models.LoadHouseRules(ctx, w.store))
	stopHeartbeat := w.startHeartbeat(ctx, task.ID)
	defer stopHeartbeat()
	if profile.RequireReview {
		if done, err := w.tryFinalizeApprovedReview(ctx, task); err != nil {
			w.FailHard(ctx, task, err)
			return
		} else if done {
			return
		}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		slog.DebugContext(ctx, "worker: handling phase planning", "task_id", task.ID)
		w.handlePhasePlanning(ctx, task, *project)
		return
	}
	if w.tryTieredOrigin(ctx, task) {
		return
	}
	if w.providerBreakers != nil && profile.Provider != "" && w.providerBreakers.Get(profile.Provider).IsOpen() {
		w.handoffOrFail(ctx, task,
			fmt.Errorf("%w: provider %s circuit breaker is open", models.ErrLLMQuotaExceeded, profile.Provider))
		return
	}
	if w.dispatchTieredStep(ctx, task, *project, *profile) {
		return
	}
	if profile.AgenticMode {
		slog.DebugContext(ctx, "worker: entering agentic loop", "task_id", task.ID)
		if result, ok := w.processAgentic(ctx, task, *project, *profile); ok {
			w.handleLoopResult(ctx, task, result)
		}
		return
	}
	slog.DebugContext(ctx, "worker: running legacy task", "task_id", task.ID)
	w.RunLegacyTask(ctx, task, *project, *profile, false)
}

func (w *Worker) tryTieredOrigin(ctx context.Context, task models.Task) bool {
	if w.isTieredStep(task) {
		return false
	}
	if w.tryResolveTieredOrigin(ctx, task) {
		return true
	}
	if !w.ShouldRunTiered(task) {
		return false
	}
	w.persistTieredDAG(ctx, task)
	return true
}

func (w *Worker) dispatchTieredStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) bool {
	if !w.isTieredStep(task) {
		return false
	}
	parents, err := w.store.ListParentTasksByRelation(ctx, task.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		slog.Error("tiered: failed to look up SPAWNED_BY parents", "task_id", task.ID, "error", err)
		w.FailHard(ctx, task, fmt.Errorf("tiered parent lookup failed: %w", err))
		return true
	}
	if len(parents) == 0 {
		slog.Error("tiered step has no SPAWNED_BY origin parent", "task_id", task.ID, "agent_id", task.AgentID)
		w.FailHard(ctx, task, fmt.Errorf("tiered step %s has no SPAWNED_BY origin parent", task.ID))
		return true
	}
	dependencies, err := w.store.ListParentTasksByRelation(ctx, task.ID, models.TaskRelationDependsOn)
	if err != nil {
		w.FailHard(ctx, task, fmt.Errorf("tiered dependency lookup failed: %w", err))
		return true
	}
	for _, dependency := range dependencies {
		if dependency.State == models.TaskStateNeedsContext {
			if w.handleNeedsContextDep(ctx, task, dependency, parents[0]) {
				return true
			}
		}
	}
	w.processTieredStep(ctx, task, project, profile, w.tieredStepKind(task), parents[0])
	return true
}

func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (LoopResult, bool) {
	engine := agentic.NewEngine(agentic.Config{
		Store: w.store, Gateway: w.gateway, Sandbox: w.sandbox,
		SandboxEnvAllowlist: w.sandboxEnvAllowlist,
		SandboxExtraEnv:     w.sandboxExtraEnv,
		SandboxWallTimeout:  w.sandboxWallTimeout,
		FileContextCfg:      w.fileContextCfg, DocStore: w.docStore,
		ContextCfg: w.contextCfg, MaxToolIterations: w.maxToolIterations,
		BudgetTracker:           w.budgetTracker,
		ContextWarningThreshold: w.contextWarningThreshold,
		ToolFailureStreak:       w.toolFailureStreak, TruncatorMax: w.truncatorMax,
		CharacterBudget: w.characterBudget, PlanningCfg: w.planningCfg,
		MessageEditor: w.messageEditor, CheckpointStore: w.checkpointStore,
		TopicGuard: w.topicGuard, ModelRouter: w.modelRouter,
		Capabilities: w.capabilities,
	}, w)
	return engine.Process(ctx, task, project, profile)
}
