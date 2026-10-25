package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"agentd/internal/aicalls"
	"agentd/internal/bus"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue"
	"agentd/internal/sandbox"
	"agentd/internal/services"
)

type runtimeDeps struct {
	bus              *bus.InProcess
	workspace        sandbox.WorkspaceManager
	emitter          *bus.EventEmitter
	gateway          gateway.AIGateway
	sandbox          sandbox.Executor
	breaker          *queue.CircuitBreaker
	providerBreakers *queue.ProviderBreakers
	canceller        *queue.CancelRegistry
	project          *services.ProjectService
	runner           *queue.TaskRunner
	aiCalls          *aicalls.Writer // nil when the store cannot record AI calls
}

func newRuntimeDeps(cfg config.Config, store models.KanbanStore) (runtimeDeps, error) {
	eventBus := bus.NewInProcess()
	ws := &sandbox.FSWorkspaceManager{Root: cfg.ProjectsDir}
	emitter := bus.NewEventEmitter(store, eventBus)
	breaker := queue.NewCircuitBreakerWithTimeout(cfg.Breaker.OpenTimeout)
	providerBreakers := queue.NewProviderBreakersWithTimeout(cfg.Breaker.OpenTimeout)
	providerConfigs, err := cfg.Gateway.ProviderConfigs()
	if err != nil {
		return runtimeDeps{}, fmt.Errorf("resolve provider configs: %w", err)
	}
	gw, err := gateway.NewRouterFromConfigs(providerConfigs)
	if err != nil {
		return runtimeDeps{}, fmt.Errorf("build gateway router: %w", err)
	}
	callWriter := attachCallRecorder(gw, store)
	if routes := cfg.Gateway.RoleRoutes(); routes != nil {
		gw = gw.WithRoleRouting(routes)
	}
	gw.WithTruncation(cfg.Gateway.TruncatorImpl(gw, breaker), cfg.Gateway.Truncator.MaxInputChars)
	ctx := context.Background()
	gateway.RunToolProbes(ctx, gw)
	for _, p := range providerConfigs {
		if gw.ProviderSupportsChatTools(p.Name) {
			slog.Info("provider supports chat tools", "provider", p.Name)
		}
	}
	slog.Info("connector topology", "topology", cfg.Gateway.ConnectorTopology(providerConfigs))
	sb := &sandbox.BashExecutor{
		Root:    cfg.ProjectsDir,
		Sink:    bus.EventBridge{Emitter: emitter},
		Backend: cfg.Sandbox.Backend,
		// B-081: toolchains installed outside /usr (e.g. ~/.local/go).
		ToolchainPaths: cfg.Sandbox.ToolchainPaths,
		Inactivity:     cfg.Sandbox.InactivityTimeout,
		KillGrace:      cfg.Sandbox.KillGrace,
		MaxLogBytes:    cfg.Sandbox.MaxLogBytes,
		Scrubber:       sandbox.NewScrubber(cfg.Sandbox.ScrubPatterns),
		Limits: sandbox.ResourceLimits{
			AddressSpaceBytes: cfg.Sandbox.Limits.AddressSpaceBytes,
			CPUSeconds:        cfg.Sandbox.Limits.CPUSeconds,
			OpenFiles:         cfg.Sandbox.Limits.OpenFiles,
			Processes:         cfg.Sandbox.Limits.Processes,
		},
	}
	return runtimeDeps{
		bus:              eventBus,
		workspace:        ws,
		emitter:          emitter,
		gateway:          gw,
		sandbox:          sb,
		breaker:          breaker,
		providerBreakers: providerBreakers,
		canceller:        queue.NewCancelRegistry(),
		project:          services.NewProjectService(store, ws).WithSourcePolicy(sandbox.SourcePolicy{Root: cfg.API.SourceRoot, AgentdHome: cfg.HomeDir}),
		runner:           queue.NewTaskRunner(gw, store, emitter, ws, cfg.ProjectsDir),
		aiCalls:          callWriter,
	}, nil
}

// recoveryWorkspaceReset returns the resetter boot reconcile uses when
// recovery.clean_workspace_on_recover is on, and nil (reset nothing) when off.
func recoveryWorkspaceReset(cfg config.Config, deps runtimeDeps) (queue.WorkspaceResetter, error) {
	if !cfg.Recovery.CleanWorkspaceOnRecover {
		return nil, nil
	}
	resetter, ok := deps.workspace.(queue.WorkspaceResetter)
	if !ok {
		return nil, fmt.Errorf("recovery.clean_workspace_on_recover: workspace manager %T cannot reset a project directory", deps.workspace)
	}
	return resetter, nil
}

// attachCallRecorder sends the router's per-attempt call records to the store
// through an async writer. It returns nil, and leaves the router's debug-log
// recorder in place, when the store does not keep ai_calls.
func attachCallRecorder(gw *gateway.Router, store models.KanbanStore) *aicalls.Writer {
	sink, ok := store.(aicalls.Sink)
	if !ok {
		return nil
	}
	writer := aicalls.NewWriter(sink, aicalls.Options{})
	gw.WithRecorder(writer)
	return writer
}

// flushAICalls writes any queued call records. Run it before the store closes.
func (d runtimeDeps) flushAICalls() {
	if d.aiCalls == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.aiCalls.Close(ctx); err != nil {
		slog.Warn("flush ai call records", "error", err)
	}
}
