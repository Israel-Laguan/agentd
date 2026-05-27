package worker

import (
	"context"
	"errors"
	"fmt"
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
	providerBreakers     *safety.ProviderBreakers
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
	tokenStore                TokenUsageStore
	loopResultRecorder        func(LoopResult)
	fileContextCfg            config.FileContextConfig
	docStore                  *DocStore
	planningCfg               config.AgenticPlanningConfig
	messageEditor             *MessageEditor
	checkpointStore           CheckpointStore
	topicGuard                *TopicGuard
	modelRouter               *ModelRouter
	toolManifest              *ToolManifest
	capabilityRouter          *CapabilityRouter
	batcher                   *TaskBatcher
	promptLibrary             *PromptLibrary
	healingEnabled            bool
	maxHealingTasks           int
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
}

// recordTaskTokenUsage feeds the optional rolling ledger hook, persists usage to
// the task row, and emits a TOKEN_USAGE audit event when a sink is wired.
func (w *Worker) recordTaskTokenUsage(ctx context.Context, task models.Task, tokens int) {
	if tokens <= 0 {
		return
	}
	if w.tokenUsageHook != nil {
		w.tokenUsageHook(tokens)
	}
	if w.tokenStore != nil {
		_ = w.tokenStore.AddTokenUsage(ctx, task.ID, tokens)
	}
	w.emit(ctx, task, string(models.EventTypeTokenUsage), fmt.Sprintf(`{"tokens":%d}`, tokens))
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
// Model routing runs once per path: routeLegacyProfile in runLegacyTask for legacy,
// applyModelRouting in processAgentic for agentic (using the pre-manifest tool registry
// for context_token_threshold; manifest filtering applies only to turn-loop requests).
// Agentic fallback to legacy reuses the agentic route (profileAlreadyRouted) so a
// second route cannot change provider.
func (w *Worker) Process(ctx context.Context, task models.Task) {
	defer w.recoverPanic(ctx, task)
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		w.failHard(ctx, task, err)
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
	// Guard: if this provider's circuit breaker is open, create an immediate
	// handoff rather than wasting a slot on a call that will fail with 429.
	if w.providerBreakers != nil && profile.Provider != "" {
		if w.providerBreakers.Get(profile.Provider).IsOpen() {
			w.handoffOrFail(ctx, task,
				fmt.Errorf("%w: provider %s circuit breaker is open",
					models.ErrLLMQuotaExceeded, profile.Provider))
			return
		}
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
	w.runLegacyTask(ctx, task, *project, *profile, false)
}

func (w *Worker) runLegacyTask(ctx context.Context, task models.Task, project models.Project, profile models.AgentProfile, profileAlreadyRouted bool) {
	if !profileAlreadyRouted {
		profile = w.routeLegacyProfile(ctx, task, project, profile)
	}
	if reason := w.legacyDispatchRejectReason(task, profile); reason != "" {
		w.createLegacyModeHandoff(ctx, task, reason, nil)
		return
	}
	response, tokenUsage, err := w.command(ctx, task, project, profile)
	w.recordTaskTokenUsage(ctx, task, tokenUsage)
	if err != nil {
		if errors.Is(err, models.ErrInvalidJSONResponse) {
			w.createLegacyModeHandoff(ctx, task, "Gateway could not return valid JSON after repair attempts.", err)
			return
		}
		w.handleGatewayError(ctx, task, err)
		return
	}
	if response.TooComplex {
		w.handleLegacyTaskBreakdown(ctx, task, response.Subtasks, false)
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
