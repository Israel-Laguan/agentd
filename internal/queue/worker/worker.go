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

// agenticProviders lists providers that support agentic mode
// (tool round-tripping with message accumulation).
var agenticProviders = []spec.Provider{
	spec.ProviderOpenAI,
	spec.ProviderAnthropic,
}

type Worker struct {
	store               models.KanbanStore
	gateway             gateway.AIGateway
	sandbox             sandbox.Executor
	breaker             *safety.CircuitBreaker
	sink                models.EventSink
	canceller           *CancelRegistry
	tuner               *planning.ParameterTuner
	retriever           MemoryRetriever
	heartbeatInterval   time.Duration
	sandboxWallTimeout  time.Duration
	sandboxEnvAllowlist []string
	sandboxExtraEnv     []string
	sandboxScrubber     sandbox.Scrubber
	maxRetries          int
	maxToolIterations   int
	truncatorMax        int
	truncationThreshold int
	characterBudget     int
	toolExecutor        *ToolExecutor
	capabilities        *capabilities.Registry
	tokenBudget         int
	budgetTracker       spec.BudgetTracker
	hooks               *HookChain
	pluginMounter       PluginMounter
	contextCfg          config.AgenticContextConfig
	instructionLoader   *InstructionLoader
	skillLoader         *SkillLoader
	skillRouter         *SkillRouter
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

type WorkerOptions struct {
	MaxRetries              int
	MaxToolIterations       int
	TokenBudget             int
	AgenticTruncatorMax     int
	AgenticTruncationThresh int
	AgenticCharacterBudget  int
	AgenticContext          config.AgenticContextConfig
	Canceller               *CancelRegistry
	Tuner                   *planning.ParameterTuner
	Retriever               MemoryRetriever
	HeartbeatInterval       time.Duration
	SandboxWallTimeout      time.Duration
	SandboxEnvAllowlist     []string
	SandboxExtraEnv         []string
	SandboxScrubPatterns    []string
	Capabilities                *capabilities.Registry
	Hooks                       *HookChain
	PluginMounter               PluginMounter
	InstructionsProjectFile     string
	InstructionsUserPrefsPath   string
	SkillsProjectDir            string
	SkillsGlobalDir             string
	SkillsThreshold             float64
	SkillsTopK                  int
}

func normalizeOpts(opts WorkerOptions) WorkerOptions {
	if opts.MaxRetries < 1 {
		opts.MaxRetries = DefaultMaxRetries
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 30 * time.Second
	}
	if opts.SandboxWallTimeout <= 0 {
		opts.SandboxWallTimeout = 10 * time.Minute
	}
	if len(opts.SandboxEnvAllowlist) == 0 {
		opts.SandboxEnvAllowlist = []string{"PATH", "HOME", "LANG", "LC_ALL", "USER"}
	}
	if len(opts.SandboxExtraEnv) == 0 {
		opts.SandboxExtraEnv = []string{"CI=true", "DEBIAN_FRONTEND=noninteractive", "NO_COLOR=1"}
	}
	if opts.MaxToolIterations <= 0 {
		opts.MaxToolIterations = config.DefaultMaxToolIterations
	}
	if opts.AgenticTruncatorMax <= 0 {
		opts.AgenticTruncatorMax = config.DefaultAgenticTruncatorMax
	}
	if opts.AgenticTruncationThresh <= 0 {
		opts.AgenticTruncationThresh = config.DefaultAgenticTruncationThreshold
	}
	if opts.AgenticCharacterBudget < 0 {
		opts.AgenticCharacterBudget = config.DefaultAgenticCharacterBudget
	}
	return opts
}

func (w *Worker) setupOptionalLoaders(opts WorkerOptions) {
	if opts.InstructionsProjectFile != "" || opts.InstructionsUserPrefsPath != "" {
		w.instructionLoader = &InstructionLoader{
			ProjectFile:         opts.InstructionsProjectFile,
			UserPreferencesPath: opts.InstructionsUserPrefsPath,
		}
	}
	if opts.SkillsProjectDir != "" || opts.SkillsGlobalDir != "" {
		w.skillLoader = &SkillLoader{
			ProjectDir: opts.SkillsProjectDir,
			GlobalDir:  opts.SkillsGlobalDir,
		}
		w.skillRouter = &SkillRouter{
			Threshold: opts.SkillsThreshold,
			TopK:      opts.SkillsTopK,
		}
	}
}

func NewWorker(
	store models.KanbanStore,
	gw gateway.AIGateway,
	sb sandbox.Executor,
	breaker *safety.CircuitBreaker,
	sink models.EventSink,
	opts WorkerOptions,
) *Worker {
	opts = normalizeOpts(opts)
	envVars := BuildSandboxEnv(opts.SandboxEnvAllowlist, opts.SandboxExtraEnv)
	var budgetTracker spec.BudgetTracker
	if opts.TokenBudget > 0 {
		budgetTracker = gateway.NewBudgetTracker(opts.TokenBudget)
	}
	scrubber := sandbox.NewScrubber(opts.SandboxScrubPatterns)
	base := resolveHooks(opts.Hooks)
	hooks := base.Clone()
	hooks.PrependPost(ScrubResultHook(scrubber))
	hooks.RegisterPost(AuditHook(sink, scrubber))

	w := &Worker{
		store: store, gateway: gw, sandbox: sb, breaker: breaker, sink: sink,
		canceller: opts.Canceller, tuner: opts.Tuner, retriever: opts.Retriever,
		heartbeatInterval:   opts.HeartbeatInterval,
		sandboxWallTimeout:  opts.SandboxWallTimeout,
		sandboxEnvAllowlist: append([]string(nil), opts.SandboxEnvAllowlist...),
		sandboxExtraEnv:     append([]string(nil), opts.SandboxExtraEnv...),
		sandboxScrubber:     scrubber,
		maxRetries:          opts.MaxRetries,
		maxToolIterations:   opts.MaxToolIterations,
		truncatorMax:        opts.AgenticTruncatorMax,
		truncationThreshold: opts.AgenticTruncationThresh,
		characterBudget:     opts.AgenticCharacterBudget,
		toolExecutor:        NewToolExecutor(sb, "", envVars, opts.SandboxWallTimeout),
		capabilities:        opts.Capabilities,
		tokenBudget:         opts.TokenBudget,
		budgetTracker:       budgetTracker,
		hooks:               hooks,
		pluginMounter:       opts.PluginMounter,
		contextCfg:          opts.AgenticContext,
	}
	w.setupOptionalLoaders(opts)
	return w
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
	if planning.IsPhasePlanningTask(task.Title) {
		w.handlePhasePlanning(ctx, task, *project)
		return
	}
	// Routing: check AgenticMode flag to determine execution path
	// Agentic mode requires provider support (see agenticProviders)
	if profile.AgenticMode {
		if w.providerSupportsAgentic(*profile) {
			w.processAgentic(ctx, task, *project, *profile)
			return
		}
		slog.Warn("agentic mode requested but provider does not support tool round-tripping; falling back to legacy mode",
			"task_id", task.ID,
			"provider", profile.Provider,
		)
	}
	response, err := w.command(ctx, task, *profile)
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
	result, runErr := w.sandbox.Execute(execCtx, w.payload(task, *project, command))
	if w.isPromptHang(result, runErr) {
		w.handlePromptRecovery(ctx, task, *project, command, result)
		return
	}
	if w.isPermissionFailure(result, runErr) {
		w.handlePermissionFailure(ctx, task, command, result)
		return
	}
	if profile.RequireReview && runErr == nil && result.Success {
		w.createReviewHandoff(ctx, task, resultPayload(result))
		return
	}
	w.commit(ctx, task, result, runErr)
}

