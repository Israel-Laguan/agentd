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
		if result, ok := w.processAgentic(ctx, task, *project, *profile); ok {
			w.handleLoopResult(ctx, task, result)
		}
		return
	}
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
		slog.Error("tiered: failed to look up SPAWNED_BY parents",
			"task_id", task.ID, "error", err)
		w.FailHard(ctx, task, fmt.Errorf("tiered parent lookup failed: %w", err))
		return true
	}
	if len(parents) == 0 {
		slog.Error("tiered step has no SPAWNED_BY origin parent",
			"task_id", task.ID, "agent_id", task.AgentID)
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
			freshDecisionID, err := w.freshDecisionID(ctx, parents[0].ID, dependency.ID)
			if err != nil {
				w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision lookup failed: %w", err))
				return true
			}
			if freshDecisionID != "" {
				fresh, err := w.store.GetTask(ctx, freshDecisionID)
				if err != nil {
					w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision fetch failed: %w", err))
					return true
				}
				switch fresh.State {
				case models.TaskStateCompleted:
					if _, err := w.store.RewireDependsOn(ctx, dependency.ID, freshDecisionID); err != nil {
						w.FailHard(ctx, task, fmt.Errorf("tiered rewire to completed decision failed: %w", err))
						return true
					}
					// Fresh decision already completed; don't leave dependent
					// parked in BLOCKED forever. Promote to READY if deps are
					// now satisfied so the next dispatch can run it.
					if w.allDependenciesResolved(ctx, task.ID) {
						latest, err := w.store.GetTask(ctx, task.ID)
						if err == nil && latest.State.CanTransitionTo(models.TaskStateReady) {
							if _, err := w.store.UpdateTaskState(ctx, latest.ID, latest.UpdatedAt, models.TaskStateReady); err != nil {
								slog.Error("tiered: failed to re-ready after completed fresh decision", "task_id", latest.ID, "error", err)
							}
						}
					}
					return true
				case models.TaskStateFailed, models.TaskStateFailedRequiresHuman:
					if _, err := w.store.RewireDependsOn(ctx, dependency.ID, freshDecisionID); err != nil {
						w.FailHard(ctx, task, fmt.Errorf("tiered rewire to failed decision failed: %w", err))
						return true
					}
					w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", freshDecisionID, fresh.State))
					return true
				case models.TaskStateNeedsContext:
					// Fresh itself is stale; fall through to look for a
					// successor or fail — do not park on a decision that
					// will never complete.
				default:
					// Active fresh decision: park and rewire.
					if task.State.CanTransitionTo(models.TaskStateBlocked) {
						if _, err := w.store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
							w.FailHard(ctx, task, fmt.Errorf("tiered park before dependency rewire failed: %w", err))
							return true
						}
					}
					if _, err := w.store.RewireDependsOn(ctx, dependency.ID, freshDecisionID); err != nil {
						w.FailHard(ctx, task, fmt.Errorf("tiered rewire to fresh decision failed: %w", err))
						return true
					}
					return true
				}
			}
			// No active fresh decision. Check for a terminal fresh that
			// already completed so we can re-ready or propagate failure
			// instead of leaving the dependent stuck.
			if completedID, err := w.freshTerminalDecisionID(ctx, parents[0].ID, dependency.ID, models.TaskStateCompleted); err == nil && completedID != "" {
				if _, err := w.store.RewireDependsOn(ctx, dependency.ID, completedID); err != nil {
					w.FailHard(ctx, task, fmt.Errorf("tiered rewire to completed decision failed: %w", err))
					return true
				}
				if w.allDependenciesResolved(ctx, task.ID) {
					latest, err := w.store.GetTask(ctx, task.ID)
					if err == nil && latest.State.CanTransitionTo(models.TaskStateReady) {
						if _, err := w.store.UpdateTaskState(ctx, latest.ID, latest.UpdatedAt, models.TaskStateReady); err != nil {
							slog.Error("tiered: failed to re-ready after completed fresh decision", "task_id", latest.ID, "error", err)
						}
					}
				}
				return true
			}
			if failedID, err := w.freshTerminalDecisionID(ctx, parents[0].ID, dependency.ID, models.TaskStateFailed); err == nil && failedID != "" {
				_, _ = w.store.RewireDependsOn(ctx, dependency.ID, failedID)
				w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", failedID, models.TaskStateFailed))
				return true
			}
			if failedHumanID, err := w.freshTerminalDecisionID(ctx, parents[0].ID, dependency.ID, models.TaskStateFailedRequiresHuman); err == nil && failedHumanID != "" {
				_, _ = w.store.RewireDependsOn(ctx, dependency.ID, failedHumanID)
				w.FailHard(ctx, task, fmt.Errorf("tiered fresh decision %s is %s", failedHumanID, models.TaskStateFailedRequiresHuman))
				return true
			}
			w.FailHard(ctx, task, fmt.Errorf("tiered step depends on stale decision %s awaiting context re-gather", dependency.ID))
			return true
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

func (w *Worker) freshDecisionID(ctx context.Context, originID, staleDecisionID string) (string, error) {
	stale, err := w.store.GetTask(ctx, staleDecisionID)
	if err != nil {
		return "", fmt.Errorf("get stale decision: %w", err)
	}
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return "", fmt.Errorf("list spawned children: %w", err)
	}
	var freshID string
	var latest time.Time
	for _, child := range children {
		if child.AgentID != tieredStepProfile[TieredStepDecision] {
			continue
		}
		if child.State == models.TaskStateCompleted || child.State == models.TaskStateFailed || child.State == models.TaskStateFailedRequiresHuman || child.State == models.TaskStateNeedsContext {
			continue
		}
		if child.CreatedAt.After(stale.CreatedAt) && child.CreatedAt.After(latest) {
			freshID = child.ID
			latest = child.CreatedAt
		}
	}
	return freshID, nil
}

func (w *Worker) freshTerminalDecisionID(ctx context.Context, originID, staleDecisionID string, terminalState models.TaskState) (string, error) {
	stale, err := w.store.GetTask(ctx, staleDecisionID)
	if err != nil {
		return "", fmt.Errorf("get stale decision: %w", err)
	}
	children, err := w.store.ListChildTasksByRelation(ctx, originID, models.TaskRelationSpawnedBy)
	if err != nil {
		return "", fmt.Errorf("list spawned children: %w", err)
	}
	var freshID string
	var latest time.Time
	for _, child := range children {
		if child.AgentID != tieredStepProfile[TieredStepDecision] {
			continue
		}
		if child.State != terminalState {
			continue
		}
		if child.CreatedAt.After(stale.CreatedAt) && child.CreatedAt.After(latest) {
			freshID = child.ID
			latest = child.CreatedAt
		}
	}
	return freshID, nil
}
