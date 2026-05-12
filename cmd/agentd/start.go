package main

import (
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"agentd/internal/api"
	"agentd/internal/config"
	"agentd/internal/frontdesk"
	"agentd/internal/memory"
	"agentd/internal/models"
	"agentd/internal/queue"
	"agentd/internal/services"

	"github.com/spf13/cobra"
)

func newStartCommand(opts *rootOptions) *cobra.Command {
	startOpts := &startOptions{}
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the agentd daemon",
		RunE:  func(cmd *cobra.Command, args []string) error { return runStartCommand(cmd, opts, startOpts) },
	}
	cmd.Flags().IntVar(&startOpts.workers, "workers", 0, "maximum concurrent workers (default: NumCPU-2)")
	return cmd
}

func runStartCommand(cmd *cobra.Command, opts *rootOptions, startOpts *startOptions) error {
	cfg, store, deps, cleanup, err := openRuntime(opts)
	if err != nil {
		return err
	}
	defer cleanup()

	store = store.WithCanceller(deps.canceller)
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	daemon, apiServer := buildStartRuntime(cfg, store, deps, startOpts)
	listener, err := net.Listen("tcp", cfg.API.Address)
	if err != nil {
		return err
	}
	defer listener.Close() //nolint:errcheck

	errCh := make(chan error, 1)
	go func() { errCh <- apiServer.Serve(listener) }()
	defer apiServer.Shutdown(ctx) //nolint:errcheck

	go func() {
		<-ctx.Done()
		_ = apiServer.Shutdown(ctx)
	}()
	go func() {
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			stop()
		}
	}()
	return daemon.Start(ctx)
}

func buildStartRuntime(cfg config.Config, store models.KanbanStore, deps runtimeDeps, startOpts *startOptions) (*queue.Daemon, *http.Server) {
	tuner := queue.NewParameterTuner(cfg.Healing)
	workerRetriever := &memory.Retriever{Store: store, Cfg: cfg.Librarian}
	worker := queue.NewWorker(store, deps.gateway, deps.sandbox, deps.breaker, deps.emitter, queue.WorkerOptions{
		Canceller:           deps.canceller,
		Tuner:               tuner,
		Retriever:           workerRetriever,
		HeartbeatInterval:   cfg.Cron.Heartbeat,
		SandboxWallTimeout:  cfg.Sandbox.WallTimeout,
		SandboxEnvAllowlist: cfg.Sandbox.EnvAllowlist,
		SandboxExtraEnv:     cfg.Sandbox.ExtraEnv,
		SandboxScrubPatterns: cfg.Sandbox.ScrubPatterns,
	})
	intake := frontdesk.NewIntakeProcessor(store, deps.gateway, deps.emitter, cfg.Gateway.TruncatorImpl(deps.gateway, deps.breaker), cfg.Gateway.Truncator.MaxInputChars)
	librarian := &memory.Librarian{
		Store:   store,
		Gateway: deps.gateway,
		Breaker: deps.breaker,
		Sink:    deps.emitter,
		Cfg:     cfg.Librarian,
		HomeDir: cfg.HomeDir,
	}
	dreamer := &memory.DreamAgent{
		Store:   store,
		Gateway: deps.gateway,
		Breaker: deps.breaker,
		Cfg:     cfg.Librarian,
	}
	daemon := queue.NewDaemon(store, worker, intake, deps.breaker, deps.emitter, queue.DaemonOptions{
		MaxWorkers:           startOpts.workers,
		TaskInterval:         cfg.Cron.TaskDispatch,
		MaxTaskInterval:      cfg.Queue.PollMaxInterval,
		TaskDeadline:         cfg.Queue.TaskDeadline,
		IntakeInterval:       cfg.Cron.Intake,
		HeartbeatInterval:    cfg.Cron.Heartbeat,
		StaleAfter:           cfg.Heartbeat.StaleAfter,
		HandoffAfter:         cfg.Breaker.HandoffAfter,
		DiskWatchdogEvery:    cfg.Cron.DiskWatchdog.Every,
		DiskWatchdogSchedule: cfg.Cron.DiskWatchdog.Schedule,
		DiskFreeThreshold:    cfg.Disk.FreeThresholdPercent,
		DiskCheckPath:        cfg.HomeDir,
		Librarian:            librarian,
		Dreamer:              dreamer,
		CuratorEvery:         cfg.Cron.MemoryCurator.Every,
		CuratorSchedule:      cfg.Cron.MemoryCurator.Schedule,
		DreamEvery:           cfg.Cron.Dream.Every,
		DreamSchedule:        cfg.Cron.Dream.Schedule,
	})
	retriever := &memory.Retriever{Store: store, Cfg: cfg.Librarian}
	summarizer := frontdesk.NewStatusSummarizer(store)
	fileStash := &frontdesk.FileStash{Dir: cfg.UploadsDir, StashThreshold: cfg.Gateway.Truncation.StashThreshold}
	board, _ := any(store).(models.KanbanBoardContract)
	taskService := services.NewTaskService(store, board)
	systemService := services.NewSystemService(summarizer, breakerProbe{breaker: deps.breaker})
	apiServer := api.NewServer(api.ServerDeps{
		Addr: cfg.API.Address, Store: store, Gateway: deps.gateway, Bus: deps.bus,
		Project: deps.project, Tasks: taskService, System: systemService,
		Summarizer: summarizer, FileStash: fileStash,
		Truncator: cfg.Gateway.TruncatorImpl(deps.gateway, deps.breaker), Budget: cfg.Gateway.Truncator.MaxInputChars,
		Retriever: retriever, MaterializeToken: cfg.API.MaterializeToken,
	})
	return daemon, apiServer
}

type startOptions struct {
	workers int
}
