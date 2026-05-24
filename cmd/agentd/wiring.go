package main

import (
	"log"

	"agentd/internal/bus"
	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/queue"
	"agentd/internal/sandbox"
	"agentd/internal/services"
)

type runtimeDeps struct {
	bus       *bus.InProcess
	workspace sandbox.WorkspaceManager
	emitter   *bus.EventEmitter
	gateway   gateway.AIGateway
	sandbox   sandbox.Executor
	breaker   *queue.CircuitBreaker
	canceller *queue.CancelRegistry
	project   *services.ProjectService
	runner    *queue.TaskRunner
}

func newRuntimeDeps(cfg config.Config, store models.KanbanStore) runtimeDeps {
	eventBus := bus.NewInProcess()
	ws := &sandbox.FSWorkspaceManager{Root: cfg.ProjectsDir}
	emitter := bus.NewEventEmitter(store, eventBus)
	breaker := queue.NewCircuitBreaker()
	providerConfigs, err := cfg.Gateway.ProviderConfigs()
	if err != nil {
		log.Fatal(err)
	}
	gw, err := gateway.NewRouterFromConfigs(providerConfigs)
	if err != nil {
		log.Fatal(err)
	}
	gw = gw.WithPhaseCap(cfg.Gateway.MaxTasksPerPhase)
	if routes := cfg.Gateway.RoleRoutes(); routes != nil {
		gw = gw.WithRoleRouting(routes)
	}
	gw.WithTruncation(cfg.Gateway.TruncatorImpl(gw, breaker), cfg.Gateway.Truncator.MaxInputChars)
	sb := &sandbox.BashExecutor{
		Root:        cfg.ProjectsDir,
		Sink:        bus.EventBridge{Emitter: emitter},
		Inactivity:  cfg.Sandbox.InactivityTimeout,
		KillGrace:   cfg.Sandbox.KillGrace,
		MaxLogBytes: cfg.Sandbox.MaxLogBytes,
		Scrubber:    sandbox.NewScrubber(cfg.Sandbox.ScrubPatterns),
		Limits: sandbox.ResourceLimits{
			AddressSpaceBytes: cfg.Sandbox.Limits.AddressSpaceBytes,
			CPUSeconds:        cfg.Sandbox.Limits.CPUSeconds,
			OpenFiles:         cfg.Sandbox.Limits.OpenFiles,
			Processes:         cfg.Sandbox.Limits.Processes,
		},
	}
	return runtimeDeps{
		bus:       eventBus,
		workspace: ws,
		emitter:   emitter,
		gateway:   gw,
		sandbox:   sb,
		breaker:   breaker,
		canceller: queue.NewCancelRegistry(),
		project:   services.NewProjectService(store, ws),
		runner:    queue.NewTaskRunner(gw, store, emitter, ws),
	}
}
