// Package queue re-exports selected symbols from queue subpackages for stable
// imports from cmd and integration tests rooted at internal/queue.
package queue

import (
	agentruntime "agentd/internal/agent/runtime"
	wsession "agentd/internal/agent/session"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/recovery"
	"agentd/internal/queue/safety"
	qw "agentd/internal/queue/worker"
)

type PIDProbe = safety.PIDProbe
type StaticPIDProbe = safety.StaticPIDProbe
type GopsutilProbe = safety.GopsutilProbe

// BootReconcile re-exports recovery.BootReconcile for stable imports from cmd
// and integration tests rooted at internal/queue.
var BootReconcile = recovery.BootReconcile

const (
	RebootRecoveryHandoffEventType = recovery.RebootRecoveryHandoffEventType
	HeartbeatReconcileEventType    = recovery.HeartbeatReconcileEventType
)

type Worker = qw.Worker
type WorkerOptions = qw.WorkerOptions
type TokenUsageStore = qw.TokenUsageStore

var NewWorker = qw.NewWorker

// EnsureAuditFile creates the audit file at path and writes a daemon_start marker.
// Call at daemon startup when agentic.audit.enabled is true.
var EnsureAuditFile = agentruntime.EnsureAuditFile

// ValidateToolCredentials checks that every mapped env var in toolCredentials is set.
func ValidateToolCredentials(toolCredentials map[string]string) error {
	if len(toolCredentials) == 0 {
		return nil
	}
	return wsession.NewEnvSecretStore(toolCredentials).Validate()
}

const DefaultWorkerMaxRetries = qw.DefaultMaxRetries

type HealingAction = planning.HealingAction

const (
	HealingActionTune  = planning.HealingActionTune
	HealingActionSplit = planning.HealingActionSplit
	HealingActionHuman = planning.HealingActionHuman
)

var IsPhasePlanningTask = planning.IsPhasePlanningTask

var NextPhaseNumber = planning.NextPhaseNumber

// RetitlePhaseContinuationTasks re-exports planning.RetitlePhaseContinuationTasks.
var RetitlePhaseContinuationTasks = planning.RetitlePhaseContinuationTasks

const (
	HealingStepLowerTemperature = planning.HealingStepLowerTemperature
	HealingStepIncreaseContext  = planning.HealingStepIncreaseContext
	HealingStepCompressContext  = planning.HealingStepCompressContext
	HealingStepUpgradeModel     = planning.HealingStepUpgradeModel
	HealingStepSplitTask        = planning.HealingStepSplitTask
	HealingStepHumanHandoff     = planning.HealingStepHumanHandoff
)

var BuildSandboxEnv = agenttools.BuildSandboxEnv

type CancelRegistry = qw.CancelRegistry

var NewCancelRegistry = qw.NewCancelRegistry

type TaskRunner = qw.TaskRunner

var NewTaskRunner = qw.NewTaskRunner

type CircuitBreaker = safety.CircuitBreaker
type BreakerState = safety.BreakerState

const (
	BreakerClosed   = safety.BreakerClosed
	BreakerOpen     = safety.BreakerOpen
	BreakerHalfOpen = safety.BreakerHalfOpen
)

var NewCircuitBreaker = safety.NewCircuitBreaker

type ProviderBreakers = safety.ProviderBreakers
type ProviderBreakerEntry = safety.ProviderBreakerEntry

var NewProviderBreakers = safety.NewProviderBreakers

// DefaultBreakerTimeout matches the circuit breaker open-state timeout.
const DefaultBreakerTimeout = safety.DefaultBreakerTimeout

var NewSemaphore = safety.NewSemaphore

type Semaphore = safety.Semaphore

type ParameterTuner = planning.ParameterTuner

var NewParameterTuner = planning.NewParameterTuner

type ChannelGateExport = ChannelGate

var NewChannelGateExport = NewChannelGate
