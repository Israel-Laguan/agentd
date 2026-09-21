package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"agentd/internal/config"
	"agentd/internal/kanban"
	"agentd/internal/mcp"
	"agentd/internal/models"
	"agentd/internal/queue"
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

	if err := seedAndValidateStartup(cmd.Context(), store, cfg, deps, startOpts); err != nil {
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

	// Start MCP server if enabled.
	if cfg.MCP.Enabled {
		startMCPServer(ctx, store, cfg.MCP)
	}

	apiErrCh := startAPIServer(ctx, listener, apiServer, stop)

	slog.Debug("starting daemon")
	if err := daemon.Start(ctx); err != nil {
		return err
	}
	return drainAPIServerError(apiErrCh)
}

// startMCPServer launches the MCP board-export server for stdio transport.
// HTTP transport is handled via buildAPIServer registering the handler on the mux.
func startMCPServer(ctx context.Context, store models.KanbanStore, mcpCfg config.MCPConfig) {
	transport := mcpCfg.Transport
	if transport == "stdio" || transport == "both" {
		mcpServer := mcp.New(store)
		go func() {
			if err := mcpServer.Run(ctx); err != nil {
				slog.Error("mcp server: stdio transport failed", "error", err)
			}
		}()
	}
	if transport == "http" || transport == "both" {
		slog.Info("mcp server: HTTP handler registered on /mcp")
	}
}

func seedAndValidateStartup(ctx context.Context, store *kanban.Store, cfg config.Config, deps runtimeDeps, startOpts *startOptions) error {
	if err := seedDefaultAgent(ctx, store, false); err != nil {
		return fmt.Errorf("seed default agent profiles: %w", err)
	}
	if err := requireStartupProviders(cfg.Gateway); err != nil {
		return err
	}
	if err := queue.ValidateToolCredentials(cfg.Agentic.ToolCredentials); err != nil {
		return fmt.Errorf("agentic.tool_credentials: %w", err)
	}
	slog.Debug("tool credentials validated")

	if err := warmupLLMIfNeeded(ctx, deps.gateway, cfg.Gateway, startOpts.skipLLMWarmup, cfg.Gateway.WarmupEnabled); err != nil {
		return err
	}
	return nil
}

type startOptions struct {
	workers       int
	skipLLMWarmup bool
}
