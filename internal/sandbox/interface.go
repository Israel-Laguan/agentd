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

// Egress is the jail option for an external agent (T-042, SP-025). The
// network stays unshared; the only way out is host unix sockets bound into
// the jail, which `agentd jail-forward` exposes on the jail's own loopback
// before the command starts: an egress.Proxy that allows only the agent's
// model hosts, and, when set, agentd's MCP server. A nil Egress is the builtin
// profile, with no network at all (B-086).
type Egress struct {
	// Forwarder is the agentd binary, bound read-only to run jail-forward.
	Forwarder string
	// ProxySocket is the host unix socket of this run's egress.Proxy.
	ProxySocket string
	// MCPSocket is the host unix socket of agentd's MCP server, served in the
	// jail at JailMCPAddr; empty when the agent spawns its MCP server over stdio.
	MCPSocket string
	// Home is the per-run writable directory bound at the same path as HOME,
	// so the agent's config, cache and sessions stay out of the workspace.
	Home string
	// Executable is the agent's binary, bound read-only at the same path.
	Executable string
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
