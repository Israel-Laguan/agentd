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

// MemoryRetriever is an optional dependency for pre-fetching durable memories.
type MemoryRetriever interface {
	Recall(ctx context.Context, intent, projectID, userID string) []models.Memory
}

// TokenUsageStore persists per-call token counts to the task row.
// It is implemented by the kanban store and injected via WorkerOptions.TokenStore.
type TokenUsageStore interface {
	AddTokenUsage(ctx context.Context, taskID string, tokens int) error
	// AddUsageDetails persists prompt-cache usage details (cached + write).
	AddUsageDetails(ctx context.Context, taskID string, details spec.UsageDetails) error
}

// recordTaskTokenUsage feeds the optional rolling ledger hook, persists usage to
// the task row, and emits a TOKEN_USAGE audit event when a sink is wired.
// Cache usage details ride along the event payload and the additive
// TokenUsageStore.AddUsageDetails seam.
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
	slog.Debug("recorded token usage",
		"task_id", task.ID, "tokens", tokens,
		"cached_tokens", details.CachedTokens, "cache_write_tokens", details.CacheWriteTokens)
	payload, err := json.Marshal(models.TokenUsagePayload{
		Tokens:           tokens,
		CachedTokens:     details.CachedTokens,
		CacheWriteTokens: details.CacheWriteTokens,
	})
	if err != nil {
		slog.Error("failed to marshal token usage payload", "task_id", task.ID, "err", err)
		return
	}
	w.Emit(ctx, task, string(models.EventTypeTokenUsage), string(payload))
}

// PluginMounter loads and mounts plugins from a directory into a
// HookChain and capabilities Registry. The worker calls this to
// mount project-scoped plugins (from workspace directories) and
// session-scoped plugins (by name from AgentProfile.Plugins).
type PluginMounter interface {
	MountProject(workspacePath string, chain *agenthooks.HookChain, registry *capabilities.Registry) error
	MountSession(names []string, chain *agenthooks.HookChain, registry *capabilities.Registry) error
}

// Process handles task execution, supporting two modes:
// - Legacy mode (default): single-shot JSON command execution via GenerateJSON
// - Agentic mode: inner loop with tool calling and message accumulation (processAgentic)
// Model routing runs once per path: routeLegacyProfile in runLegacyTask for legacy,
// applyModelRouting in processAgentic for agentic (using the pre-manifest tool registry
// for context_token_threshold; manifest filtering applies only to turn-loop requests).
// Agentic fallback to legacy reuses the agentic route (profileAlreadyRouted) so a
// second route cannot change provider.
func (w *Worker) Process(ctx context.Context, task models.Task) {
	defer w.recoverPanic(ctx, task)
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		w.FailHard(ctx, task, err)
		return
	}
	w.warnIfWorkspaceEmpty(ctx, task, project)
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
			w.FailHard(ctx, task, err)
			return
		} else if done {
			return
		}
	}
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, *project)
		return
	}
	// Tiered pipeline gate: when enabled and the task exceeds the complexity
	// threshold, split it into a context→decision→execute→verify DAG and
	// block the parent. The child tasks will be picked up by the queue and
	// dispatched through processTieredStep below.
	if !w.isTieredStep(task) && w.ShouldRunTiered(task) {
		w.persistTieredDAG(ctx, task)
		return
	}
	// Guard: if this provider's circuit breaker is open, create an immediate
	// handoff rather than wasting a slot on a call that will fail with 429.
	if w.providerBreakers != nil && profile.Provider != "" && w.providerBreakers.Get(profile.Provider).IsOpen() {
		w.handoffOrFail(ctx, task,
			fmt.Errorf("%w: provider %s circuit breaker is open", models.ErrLLMQuotaExceeded, profile.Provider))
		return
	}
	// Tiered step detection: child tasks created by SplitIntoTieredDAG carry
	// an AgentID matching a tiered step profile (tier-context, tier-decision,
	// tier-execute, tier-verify). Dispatch them through the tiered pipeline.
	if w.tryDispatchTieredStep(ctx, task, *project, *profile) {
		return
	}
	// AgenticMode selects processAgentic. Model routing (Task 43) and external capability
	// routing (Task 45) run inside processAgentic after tools are assembled; capability
	// routing intercepts before the agentic turn loop when a mapped adapter is available.
	if profile.AgenticMode {
		if result, ok := w.processAgentic(ctx, task, *project, *profile); ok {
			w.handleLoopResult(ctx, task, result)
		}
		return
	}
	w.RunLegacyTask(ctx, task, *project, *profile, false)
}

// tryDispatchTieredStep resolves the origin task for a tiered step child
// (context, decision, execute, or verify) and dispatches it through the
// tiered pipeline. It reports whether the task was dispatched; the caller
// falls back to the legacy/agentic path when it returns false.
func (w *Worker) tryDispatchTieredStep(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) bool {
	if !w.isTieredStep(task) {
		return false
	}
	// Resolve the origin task via the SPAWNED_BY relation. Every tiered
	// step is connected to the original pipeline parent by a SPAWNED_BY
	// edge; DEPENDS_ON edges connect consecutive steps within the chain.
	parents, err := w.store.ListParentTasksByRelation(ctx, task.ID, models.TaskRelationSpawnedBy)
	if err != nil {
		slog.Error("tiered: failed to look up SPAWNED_BY parents",
			"task_id", task.ID, "error", err)
		w.FailHard(ctx, task, fmt.Errorf("tiered SPAWNED_BY lookup failed: %w", err))
		return true
	}
	if len(parents) == 0 {
		slog.Error("tiered step has no SPAWNED_BY origin parent",
			"task_id", task.ID, "agent_id", task.AgentID)
		w.FailHard(ctx, task, fmt.Errorf("tiered step %s has no SPAWNED_BY origin parent", task.ID))
		return true
	}
	w.processTieredStep(ctx, task, project, profile, w.tieredStepKind(task), parents[0])
	return true
}

func (w *Worker) processAgentic(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile) (LoopResult, bool) {
	engine := agentic.NewEngine(agentic.Config{
		Store:                   w.store,
		Gateway:                 w.gateway,
		Sandbox:                 w.sandbox,
		SandboxEnvAllowlist:     w.sandboxEnvAllowlist,
		SandboxExtraEnv:         w.sandboxExtraEnv,
		SandboxWallTimeout:      w.sandboxWallTimeout,
		FileContextCfg:          w.fileContextCfg,
		DocStore:                w.docStore,
		ContextCfg:              w.contextCfg,
		MaxToolIterations:       w.maxToolIterations,
		BudgetTracker:           w.budgetTracker,
		ContextWarningThreshold: w.contextWarningThreshold,
		ToolFailureStreak:       w.toolFailureStreak,
		TruncatorMax:            w.truncatorMax,
		CharacterBudget:         w.characterBudget,
		PlanningCfg:             w.planningCfg,
		MessageEditor:           w.messageEditor,
		CheckpointStore:         w.checkpointStore,
		TopicGuard:              w.topicGuard,
		ModelRouter:             w.modelRouter,
		Capabilities:            w.capabilities,
	}, w)
	return engine.Process(ctx, task, project, profile)
}
