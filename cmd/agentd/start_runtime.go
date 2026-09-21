package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"agentd/internal/api"
	"agentd/internal/config"
	"agentd/internal/frontdesk"
	"agentd/internal/mcp"
	"agentd/internal/memory"
	"agentd/internal/models"
	"agentd/internal/queue"
	"agentd/internal/services"
)

func buildStartRuntime(ctx context.Context, cfg config.Config, store models.KanbanStore, deps runtimeDeps, startOpts *startOptions) (*queue.Daemon, *http.Server, error) {
	ledger := queue.NewRollingTokenLedger(cfg.Queue.RollingTokenWindow, cfg.Queue.RollingTokenLimit)
	hydrateRollingLedger(ctx, store, ledger)
	worker := buildWorker(store, deps, cfg, ledger)
	intake := buildIntake(store, deps, cfg)
	daemon, err := buildDaemon(ctx, store, worker, intake, deps, cfg, startOpts, ledger)
	if err != nil {
		return nil, nil, err
	}
	server, err := buildAPIServer(store, deps, cfg, ledger)
	if err != nil {
		return nil, nil, err
	}
	return daemon, server, nil
}

func buildWorker(store models.KanbanStore, deps runtimeDeps, cfg config.Config, ledger *queue.RollingTokenLedger) *queue.Worker {
	path := ""
	if cfg.Queue.Instructions.UserPreferencesFile != "" {
		path = filepath.Join(cfg.HomeDir, cfg.Queue.Instructions.UserPreferencesFile)
	}
	var hook func(int)
	if ledger != nil && ledger.Enabled() {
		hook = func(tokens int) { ledger.LogCall(tokens) }
	}
	return queue.NewWorker(store, deps.gateway, deps.sandbox, deps.breaker, deps.emitter, buildWorkerOptions(store, deps, cfg, path, hook))
}

func buildWorkerOptions(store models.KanbanStore, deps runtimeDeps, cfg config.Config, userPrefsPath string, tokenHook func(int)) queue.WorkerOptions {
	r := &memory.Retriever{Store: store, Cfg: cfg.Librarian}
	ts, _ := store.(queue.TokenUsageStore)
	return queue.WorkerOptions{Canceller: deps.canceller, Tuner: queue.NewParameterTuner(cfg.Healing), Retriever: r, HeartbeatInterval: cfg.Cron.Heartbeat, SandboxWallTimeout: cfg.Sandbox.WallTimeout, SandboxEnvAllowlist: cfg.Sandbox.EnvAllowlist, SandboxExtraEnv: cfg.Sandbox.ExtraEnv, SandboxScrubPatterns: cfg.Sandbox.ScrubPatterns, MaxToolIterations: cfg.Queue.MaxToolIterations, TokenBudget: cfg.Queue.TokenBudget, AgenticTruncatorMax: cfg.Queue.AgenticTruncatorMax, AgenticCharacterBudget: config.EffectiveAgenticCharacterBudget(cfg.Queue.AgenticCharacterBudget, cfg.Gateway.Truncator.MaxInputChars), AgenticContext: cfg.Queue.AgenticContext, InstructionsProjectFile: cfg.Queue.Instructions.ProjectFile, InstructionsUserPrefsPath: userPrefsPath, SkillsProjectDir: cfg.Queue.Skills.ProjectDir, SkillsGlobalDir: cfg.Queue.Skills.GlobalDir, SkillsThreshold: cfg.Queue.Skills.Threshold, SkillsTopK: cfg.Queue.Skills.TopK, LegacyHandoffTimeout: cfg.Queue.HITL.LegacyHandoffTimeout, ToolTimeouts: cfg.Queue.ToolTimeouts, ToolRetries: cfg.Queue.ToolRetries, ExternalTools: cfg.Agentic.ExternalTools, ToolCredentials: cfg.Agentic.ToolCredentials, DisableCredentialDetection: cfg.Agentic.DisableCredentialDetection, Audit: config.AuditConfig{Enabled: cfg.Agentic.Audit.Enabled, Path: config.ResolveAuditPath(cfg.HomeDir, cfg.Agentic.Audit.Path)}, ContextWarningThreshold: cfg.Agentic.ContextWarningThreshold, ToolFailureStreak: cfg.Agentic.ToolFailureStreak, TokenUsageHook: tokenHook, TokenStore: ts, FileContext: cfg.Agentic.FileContext, FileContextCachePath: config.ResolveFileContextCachePath(cfg.HomeDir, cfg.Agentic.FileContext.CachePath), Planning: cfg.Agentic.Planning, Tiered: cfg.Tiered, TopicGuard: cfg.Agentic.TopicGuard, ModelRouting: cfg.Agentic.ModelRouting, ProviderBreakers: deps.providerBreakers, HealingDisabled: !cfg.Healing.Enabled, MaxHealingTasks: cfg.Healing.MaxHealingTasks, Legacy: cfg.Queue.Legacy}
}

func buildIntake(store models.KanbanStore, deps runtimeDeps, cfg config.Config) *frontdesk.IntakeProcessor {
	return frontdesk.NewIntakeProcessor(store, deps.gateway, deps.emitter, cfg.Gateway.TruncatorImpl(deps.gateway, deps.breaker), cfg.Gateway.Truncator.MaxInputChars)
}

func buildLibrarian(store models.KanbanStore, deps runtimeDeps, cfg config.Config) *memory.Librarian {
	return &memory.Librarian{Store: store, Gateway: deps.gateway, Breaker: deps.breaker, Sink: deps.emitter, Cfg: cfg.Librarian, HomeDir: cfg.HomeDir}
}

func buildDreamer(store models.KanbanStore, deps runtimeDeps, cfg config.Config) *memory.DreamAgent {
	return &memory.DreamAgent{Store: store, Gateway: deps.gateway, Breaker: deps.breaker, Cfg: cfg.Librarian}
}

func buildDaemon(ctx context.Context, store models.KanbanStore, worker *queue.Worker, intake *frontdesk.IntakeProcessor, deps runtimeDeps, cfg config.Config, opts *startOptions, ledger *queue.RollingTokenLedger) (*queue.Daemon, error) {
	var requeue time.Duration
	if cfg.Channel.RateLimit > 0 {
		requeue = time.Duration(config.NormalizedRateWindow(cfg.Channel)) * time.Second
	}
	var channel queue.Channel
	if config.ChannelGateEnabled(cfg.Channel) {
		channel = queue.NewChannelGate(cfg.Channel)
	}
	var scheduler *queue.Scheduler
	if cfg.Agentic.Scheduler.Enabled {
		var err error
		scheduler, err = queue.NewSchedulerFromConfig(ctx, store, deps.emitter, cfg.Agentic, cfg.Librarian)
		if err != nil {
			return nil, fmt.Errorf("scheduler init: %w", err)
		}
	}
	outage := cfg.Healing.OutageHandoffEnabled
	return queue.NewDaemon(store, worker, intake, deps.breaker, deps.emitter, queue.DaemonOptions{OutageHandoffEnabled: &outage, MaxWorkers: opts.workers, TaskInterval: cfg.Cron.TaskDispatch, MaxTaskInterval: cfg.Queue.PollMaxInterval, TaskDeadline: cfg.Queue.TaskDeadline, IntakeInterval: cfg.Cron.Intake, HeartbeatInterval: cfg.Cron.Heartbeat, StaleAfter: cfg.Heartbeat.StaleAfter, HandoffAfter: cfg.Breaker.HandoffAfter, DiskWatchdogEvery: cfg.Cron.DiskWatchdog.Every, DiskWatchdogSchedule: cfg.Cron.DiskWatchdog.Schedule, HITLReconcileEvery: cfg.Cron.HITLReconcile.Every, HITLReconcileSchedule: cfg.Cron.HITLReconcile.Schedule, DiskFreeThreshold: cfg.Disk.FreeThresholdPercent, DiskCheckPath: cfg.HomeDir, Librarian: buildLibrarian(store, deps, cfg), Dreamer: buildDreamer(store, deps, cfg), CuratorEvery: cfg.Cron.MemoryCurator.Every, CuratorSchedule: cfg.Cron.MemoryCurator.Schedule, DreamEvery: cfg.Cron.Dream.Every, DreamSchedule: cfg.Cron.Dream.Schedule, Channel: channel, QueuedReconcileAfter: cfg.Queue.QueuedReconcileAfter, RateLimitedRequeueAfter: requeue, RollingTokenLedger: ledger, Scheduler: scheduler}), nil
}

func buildAPIServer(store models.KanbanStore, deps runtimeDeps, cfg config.Config, ledger *queue.RollingTokenLedger) (*http.Server, error) {
	retriever := &memory.Retriever{Store: store, Cfg: cfg.Librarian}
	summarizer := frontdesk.NewStatusSummarizer(store)
	stash := &frontdesk.FileStash{Dir: cfg.UploadsDir, StashThreshold: cfg.Gateway.Truncation.StashThreshold}
	board, _ := any(store).(models.KanbanBoardContract)
	tasks := services.NewTaskService(store, board)
	system := services.NewSystemService(summarizer, breakerProbe{breaker: deps.breaker})
	system.Resetter = breakerProbe{breaker: deps.breaker}
	system.ProviderBreakers = providerBreakersProbe{pb: deps.providerBreakers}
	if tc, ok := store.(services.TokenCounter); ok {
		system.TokenCounter = tc
	}
	if ledger != nil {
		system.RollingBudget = rollingBudgetProbe{ledger: ledger, window: cfg.Queue.RollingTokenWindow}
	}
	providers, err := cfg.Gateway.ProviderConfigs()
	if err != nil {
		return nil, fmt.Errorf("gateway provider configs: %w", err)
	}
	var mcpHandler http.Handler
	if cfg.MCP.Enabled && (cfg.MCP.Transport == "http" || cfg.MCP.Transport == "both") {
		mcpHandler = mcp.New(store).HTTPHandler()
	}
	return &http.Server{Addr: cfg.API.Address, Handler: api.NewHandler(api.ServerDeps{Addr: cfg.API.Address, Store: store, Gateway: deps.gateway, Bus: deps.bus, Project: deps.project, Tasks: tasks, System: system, Summarizer: summarizer, FileStash: stash, Truncator: cfg.Gateway.TruncatorImpl(deps.gateway, deps.breaker), Budget: cfg.Gateway.Truncator.MaxInputChars, Retriever: retriever, MaterializeToken: cfg.API.MaterializeToken, ProviderConfigs: providers, MCPHandler: mcpHandler})}, nil
}
