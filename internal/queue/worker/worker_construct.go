package worker

import (
	"log/slog"
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

type WorkerOptions struct {
	MaxRetries                int
	MaxToolIterations         int
	TokenBudget               int
	AgenticTruncatorMax       int
	AgenticCharacterBudget    int
	AgenticContext            config.AgenticContextConfig
	Canceller                 *CancelRegistry
	Tuner                     *planning.ParameterTuner
	Retriever                 MemoryRetriever
	HeartbeatInterval         time.Duration
	SandboxWallTimeout        time.Duration
	SandboxEnvAllowlist       []string
	SandboxExtraEnv           []string
	SandboxScrubPatterns      []string
	Capabilities              *capabilities.Registry
	Hooks                     *HookChain
	PluginMounter             PluginMounter
	InstructionsProjectFile   string
	InstructionsUserPrefsPath string
	SkillsProjectDir          string
	SkillsGlobalDir           string
	SkillsThreshold           float64
	SkillsTopK                int
	LegacyHandoffTimeout      time.Duration
	ToolTimeouts              config.ToolTimeoutsConfig
	ToolRetries               config.ToolRetriesConfig
	ExternalTools             []string
	ToolCredentials           map[string]string
	DisableCredentialDetection bool
	Audit                     config.AuditConfig
	ContextWarningThreshold   float64
	ToolFailureStreak         int
	TokenUsageHook            func(int)
	FileContext               config.FileContextConfig
	FileContextCachePath      string
	Planning                  config.AgenticPlanningConfig
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
	if opts.AgenticCharacterBudget < 0 {
		opts.AgenticCharacterBudget = config.DefaultAgenticCharacterBudget
	}
	if opts.LegacyHandoffTimeout <= 0 {
		opts.LegacyHandoffTimeout = config.DefaultLegacyHandoffTimeout
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
	toolExecutor := NewToolExecutor(sb, "", envVars, opts.SandboxWallTimeout)
	hooks := buildWorkerHooks(opts, toolExecutor, sink, scrubber)

	w := &Worker{
		store: store, gateway: gw, sandbox: sb, breaker: breaker, sink: sink,
		canceller: opts.Canceller, tuner: opts.Tuner, retriever: opts.Retriever,
		heartbeatInterval:    opts.HeartbeatInterval,
		sandboxWallTimeout:   opts.SandboxWallTimeout,
		sandboxEnvAllowlist:  append([]string(nil), opts.SandboxEnvAllowlist...),
		sandboxExtraEnv:      append([]string(nil), opts.SandboxExtraEnv...),
		sandboxScrubber:      scrubber,
		maxRetries:           opts.MaxRetries,
		maxToolIterations:    opts.MaxToolIterations,
		truncatorMax:         opts.AgenticTruncatorMax,
		characterBudget:      opts.AgenticCharacterBudget,
		toolExecutor:         toolExecutor,
		toolTimeouts:         opts.ToolTimeouts,
		toolRetries:          opts.ToolRetries,
		toolRetrier: NewRetryingExecutor(RetryConfig{
			MaxAttempts: opts.ToolRetries.MaxAttempts,
			BaseDelay:   opts.ToolRetries.BaseDelay,
			MaxDelay:    opts.ToolRetries.MaxDelay,
		}),
		capabilities:         opts.Capabilities,
		tokenBudget:          opts.TokenBudget,
		budgetTracker:        budgetTracker,
		hooks:                hooks,
		pluginMounter:        opts.PluginMounter,
		contextCfg:           opts.AgenticContext,
		legacyHandoffTimeout: opts.LegacyHandoffTimeout,
		externalTools:           externalToolsSet(opts.ExternalTools),
		auditLogger:             newAuditLogger(opts.Audit),
		contextWarningThreshold: opts.ContextWarningThreshold,
		toolFailureStreak:       opts.ToolFailureStreak,
		tokenUsageHook:          opts.TokenUsageHook,
		fileContextCfg:          opts.FileContext,
		planningCfg:             opts.Planning,
	}
	if opts.FileContext.Enabled && opts.FileContextCachePath != "" {
		if docStore, err := NewDocStore(opts.FileContextCachePath); err == nil {
			w.docStore = docStore
		} else {
			slog.Warn("file context cache disabled", "error", err)
		}
	}
	w.setupOptionalLoaders(opts)
	return w
}
