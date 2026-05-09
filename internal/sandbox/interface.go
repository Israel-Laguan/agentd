package sandbox

import (
	"context"

	"agentd/internal/models"
)

// Payload is the contract passed to the physical execution layer.
type Payload = models.ExecutionPayload

type ResourceLimits struct {
	AddressSpaceBytes uint64
	CPUSeconds        uint64
	OpenFiles         uint64
	Processes         uint64
}

// Result captures the physical outcome of a command.
type Result = models.ExecutionResult

// Executor is the physical command execution boundary.
type Executor interface {
	Execute(ctx context.Context, payload Payload) (Result, error)
}

// EnvironmentAdapter bridges Executor to the proposal-aligned sandbox contract.
type EnvironmentAdapter struct {
	Executor Executor
}

var _ models.SandboxEnvironment = (*EnvironmentAdapter)(nil)

func (a EnvironmentAdapter) Execute(ctx context.Context, payload models.ExecutionPayload) models.ExecutionResult {
	if a.Executor == nil {
		return models.ExecutionResult{Success: false, FatalError: "sandbox executor is not configured"}
	}
	result, err := a.Executor.Execute(ctx, payload)
	if err != nil {
		result.FatalError = err.Error()
		if result.Output == "" {
			result.Output = result.Stderr
		}
	}
	if result.Output == "" {
		result.Output = result.Stdout
	}
	return result
}

func (a EnvironmentAdapter) CleanupZombie(pid int) error {
	return terminateProcessGroup(pid, defaultKillGrace)
}
