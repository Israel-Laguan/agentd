package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"agentd/internal/api"
	"agentd/internal/config"
	"agentd/internal/frontdesk"
	"agentd/internal/memory"
	"agentd/internal/models"
	"agentd/internal/queue"
	"agentd/internal/services"
)

func newStartCommand(opts *rootOptions) *cobra.Command {
	startOpts := &startOptions{}
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the agentd daemon",
		RunE:  func(cmd *cobra.Command, args []string) error { return runStartCommand(cmd, opts, startOpts) },
	}
	cmd.Flags().IntVar(&startOpts.workers, "workers", 0, "maximum concurrent workers (default: NumCPU-2)")
	cmd.Flags().BoolVar(&startOpts.skipLLMWarmup, "skip-llm-warmup", false, "skip the billable LLM warmup request on startup")
	return cmd
}

func runStartCommand(cmd *cobra.Command, opts *rootOptions, startOpts *startOptions) error {
	cfg, store, deps, cleanup, err := openRuntime(opts)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := requireStartupProviders(cfg.Gateway); err != nil {
		return err
	}

	if err := queue.ValidateToolCredentials(cfg.Agentic.ToolCredentials); err != nil {
		return fmt.Errorf("agentic.tool_credentials: %w", err)
	}
	slog.Debug("tool credentials validated")

	if err := warmupLLMIfNeeded(cmd.Context(), deps.gateway, cfg.Gateway, startOpts.skipLLMWarmup, cfg.Gateway.WarmupEnabled); err != nil {
		return err
	}

	store = store.WithCanceller(deps.canceller)
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Debug("building daemon and API server")
	daemon, apiServer, err := buildStartRuntime(ctx, cfg, store, deps, startOpts)
	if err != nil {
		return fmt.Errorf("build daemon and API server: %w", err)
	}

	listener, err := net.Listen("tcp", cfg.API.Address)
	if err != nil {
		return fmt.Errorf("listen on API address %s: %w", cfg.API.Address, err)
	}
	defer listener.Close() //nolint:errcheck
	slog.Info("API server listening", "address", listener.Addr().String())

	if cfg.Agentic.Audit.Enabled {
		auditPath := config.ResolveAuditPath(cfg.HomeDir, cfg.Agentic.Audit.Path)
		if err := queue.EnsureAuditFile(auditPath); err != nil {
			slog.Error("failed to initialize audit file; audit events will not be written",
				"path", auditPath, "error", err)
		}
	}
	slog.Debug("HTTP server started")

	apiErrCh := startAPIServer(ctx, listener, apiServer, stop)

	slog.Debug("starting daemon")
	if err := daemon.Start(ctx); err != nil {
		return err
	}
	return drainAPIServerError(apiErrCh)
}

func buildStartRuntime(ctx context.Context, cfg config.Config, store models.KanbanStore, deps runtimeDeps, startOpts *startOptions) (*queue.Daemon, *http.Server, error) {
	rollingLedger := queue.NewRollingTokenLedger(cfg.Queue.RollingTokenWindow, cfg.Queue.RollingTokenLimit)
	hydrateRollingLedger(ctx, store, rollingLedger)
	worker := buildWorker(store, deps, cfg, rollingLedger)
	intake := buildIntake(store, deps, cfg)
	daemon, err := buildDaemon(ctx, store, worker, intake, deps, cfg, startOpts, rollingLedger)
	if err != nil {
		return nil, nil, err
	}
	apiServer, err := buildAPIServer(store, deps, cfg, rollingLedger)
	if err != nil {
		return nil, nil, err
	}
	return daemon, apiServer, nil
}

func buildWorker(store models.KanbanStore, deps runtimeDeps, cfg config.Config, rollingLedger *queue.RollingTokenLedger) *queue.Worker {
	workerRetriever := &memory.Retriever{Store: store, Cfg: cfg.Librarian}

	userPrefsPath := ""
	if cfg.Queue.Instructions.UserPreferencesFile != "" {
		userPrefsPath = filepath.Join(cfg.HomeDir, cfg.Queue.Instructions.UserPreferencesFile)
	}

	var tokenHook func(int)
	if rollingLedger != nil && rollingLedger.Enabled() {
		ledger := rollingLedger
		tokenHook = func(tokens int) { ledger.LogCall(tokens) }
	}
	return queue.NewWorker(store, deps.gateway, deps.sandbox, deps.breaker, deps.emitter, queue.WorkerOptions{
		Canceller:                 deps.canceller,
		Tuner:                     queue.NewParameterTuner(cfg.Healing),
		Retriever:                 workerRetriever,
		HeartbeatInterval:         cfg.Cron.Heartbeat,
		SandboxWallTimeout:        cfg.Sandbox.WallTimeout,
		SandboxEnvAllowlist:       cfg.Sandbox.EnvAllowlist,
		SandboxExtraEnv:           cfg.Sandbox.ExtraEnv,
		SandboxScrubPatterns:      cfg.Sandbox.ScrubPatterns,
		MaxToolIterations:         cfg.Queue.MaxToolIterations,
		TokenBudget:               cfg.Queue.TokenBudget,
		AgenticTruncatorMax: cfg.Queue.AgenticTruncatorMax,
		AgenticCharacterBudget: config.EffectiveAgenticCharacterBudget(
			cfg.Queue.AgenticCharacterBudget,
			cfg.Gateway.Truncator.MaxInputChars,
		),
		AgenticContext:            cfg.Queue.AgenticContext,
		InstructionsProjectFile:   cfg.Queue.Instructions.ProjectFile,
		InstructionsUserPrefsPath: userPrefsPath,
		SkillsProjectDir:          cfg.Queue.Skills.ProjectDir,
		SkillsGlobalDir:           cfg.Queue.Skills.GlobalDir,
		SkillsThreshold:           cfg.Queue.Skills.Threshold,
		SkillsTopK:                cfg.Queue.Skills.TopK,
		LegacyHandoffTimeout:      cfg.Queue.HITL.LegacyHandoffTimeout,
		ToolTimeouts:              cfg.Queue.ToolTimeouts,
		ToolRetries:               cfg.Queue.ToolRetries,
		ExternalTools:             cfg.Agentic.ExternalTools,
		ToolCredentials:              cfg.Agentic.ToolCredentials,
		DisableCredentialDetection:     cfg.Agentic.DisableCredentialDetection,
		Audit: config.AuditConfig{
			Enabled: cfg.Agentic.Audit.Enabled,
			Path:    config.ResolveAuditPath(cfg.HomeDir, cfg.Agentic.Audit.Path),
		},
		ContextWarningThreshold: cfg.Agentic.ContextWarningThreshold,
		ToolFailureStreak:       cfg.Agentic.ToolFailureStreak,
		TokenUsageHook:          tokenHook,
		TokenStore:              tokenUsageStore(store),
		FileContext:               cfg.Agentic.FileContext,
		FileContextCachePath: config.ResolveFileContextCachePath(cfg.HomeDir, cfg.Agentic.FileContext.CachePath),
		Planning:                  cfg.Agentic.Planning,
		TopicGuard:                cfg.Agentic.TopicGuard,
		ModelRouting:              cfg.Agentic.ModelRouting,
		ProviderBreakers:          deps.providerBreakers,
		HealingDisabled:           !cfg.Healing.Enabled,
		MaxHealingTasks:           cfg.Healing.MaxHealingTasks,
		Legacy:                    cfg.Queue.Legacy,
	})
}

func buildIntake(store models.KanbanStore, deps runtimeDeps, cfg config.Config) *frontdesk.IntakeProcessor {
	return frontdesk.NewIntakeProcessor(
		store, deps.gateway, deps.emitter,
		cfg.Gateway.TruncatorImpl(deps.gateway, deps.breaker),
		cfg.Gateway.Truncator.MaxInputChars,
	)
}

func buildLibrarian(store models.KanbanStore, deps runtimeDeps, cfg config.Config) *memory.Librarian {
	return &memory.Librarian{
		Store:   store,
		Gateway: deps.gateway,
		Breaker: deps.breaker,
		Sink:    deps.emitter,
		Cfg:     cfg.Librarian,
		HomeDir: cfg.HomeDir,
	}
}

func buildDreamer(store models.KanbanStore, deps runtimeDeps, cfg config.Config) *memory.DreamAgent {
	return &memory.DreamAgent{
		Store:   store,
		Gateway: deps.gateway,
		Breaker: deps.breaker,
		Cfg:     cfg.Librarian,
	}
}

func buildDaemon(ctx context.Context, store models.KanbanStore, worker *queue.Worker, intake *frontdesk.IntakeProcessor, deps runtimeDeps, cfg config.Config, startOpts *startOptions, rollingLedger *queue.RollingTokenLedger) (*queue.Daemon, error) {
	var rateLimitedRequeueAfter time.Duration
	if cfg.Channel.RateLimit > 0 {
		rateLimitedRequeueAfter = time.Duration(config.NormalizedRateWindow(cfg.Channel)) * time.Second
	}
	var ch queue.Channel
	if config.ChannelGateEnabled(cfg.Channel) {
		ch = queue.NewChannelGate(cfg.Channel)
	}
	var scheduler *queue.Scheduler
	if cfg.Agentic.Scheduler.Enabled {
		slog.Debug("initializing agentic scheduler")
		var schedErr error
		scheduler, schedErr = queue.NewSchedulerFromConfig(ctx, store, deps.emitter, cfg.Agentic, cfg.Librarian)
		if schedErr != nil {
			return nil, fmt.Errorf("scheduler init: %w", schedErr)
		}
		slog.Debug("agentic scheduler initialized")
	}

	outageHandoff := cfg.Healing.OutageHandoffEnabled
	return queue.NewDaemon(store, worker, intake, deps.breaker, deps.emitter, queue.DaemonOptions{
		OutageHandoffEnabled:    &outageHandoff,
		MaxWorkers:              startOpts.workers,
		TaskInterval:            cfg.Cron.TaskDispatch,
		MaxTaskInterval:         cfg.Queue.PollMaxInterval,
		TaskDeadline:            cfg.Queue.TaskDeadline,
		IntakeInterval:          cfg.Cron.Intake,
		HeartbeatInterval:       cfg.Cron.Heartbeat,
		StaleAfter:              cfg.Heartbeat.StaleAfter,
		HandoffAfter:            cfg.Breaker.HandoffAfter,
		DiskWatchdogEvery:       cfg.Cron.DiskWatchdog.Every,
		DiskWatchdogSchedule:    cfg.Cron.DiskWatchdog.Schedule,
		HITLReconcileEvery:      cfg.Cron.HITLReconcile.Every,
		HITLReconcileSchedule:   cfg.Cron.HITLReconcile.Schedule,
		DiskFreeThreshold:       cfg.Disk.FreeThresholdPercent,
		DiskCheckPath:           cfg.HomeDir,
		Librarian:               buildLibrarian(store, deps, cfg),
		Dreamer:                 buildDreamer(store, deps, cfg),
		CuratorEvery:            cfg.Cron.MemoryCurator.Every,
		CuratorSchedule:         cfg.Cron.MemoryCurator.Schedule,
		DreamEvery:              cfg.Cron.Dream.Every,
		DreamSchedule:           cfg.Cron.Dream.Schedule,
		Channel:                 ch,
		QueuedReconcileAfter:    cfg.Queue.QueuedReconcileAfter,
		RateLimitedRequeueAfter: rateLimitedRequeueAfter,
		RollingTokenLedger:      rollingLedger,
		Scheduler:               scheduler,
	}), nil
}

func buildAPIServer(store models.KanbanStore, deps runtimeDeps, cfg config.Config, rollingLedger *queue.RollingTokenLedger) (*http.Server, error) {
	retriever := &memory.Retriever{Store: store, Cfg: cfg.Librarian}
	summarizer := frontdesk.NewStatusSummarizer(store)
	fileStash := &frontdesk.FileStash{Dir: cfg.UploadsDir, StashThreshold: cfg.Gateway.Truncation.StashThreshold}
	board, _ := any(store).(models.KanbanBoardContract)
	taskService := services.NewTaskService(store, board)
	systemService := services.NewSystemService(summarizer, breakerProbe{breaker: deps.breaker})
	systemService.Resetter = breakerProbe{breaker: deps.breaker}
	systemService.ProviderBreakers = providerBreakersProbe{pb: deps.providerBreakers}
	if tc, ok := store.(services.TokenCounter); ok {
		systemService.TokenCounter = tc
	}
	if rollingLedger != nil {
		systemService.RollingBudget = rollingBudgetProbe{ledger: rollingLedger, window: cfg.Queue.RollingTokenWindow}
	}
	providerCfgs, err := cfg.Gateway.ProviderConfigs()
	if err != nil {
		return nil, fmt.Errorf("gateway provider configs: %w", err)
	}
	return api.NewServer(api.ServerDeps{
		Addr: cfg.API.Address, Store: store, Gateway: deps.gateway, Bus: deps.bus,
		Project: deps.project, Tasks: taskService, System: systemService,
		Summarizer: summarizer, FileStash: fileStash,
		Truncator: cfg.Gateway.TruncatorImpl(deps.gateway, deps.breaker), Budget: cfg.Gateway.Truncator.MaxInputChars,
		Retriever: retriever, MaterializeToken: cfg.API.MaterializeToken,
		ProviderConfigs: providerCfgs,
	}), nil
}

type startOptions struct {
	workers        int
	skipLLMWarmup  bool
}
