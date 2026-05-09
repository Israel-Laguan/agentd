// Package queue re-exports selected symbols from queue subpackages for stable
// imports from cmd and integration tests rooted at internal/queue.
package queue

import (
	"context"

	"agentd/internal/models"
	"agentd/internal/queue/planning"
	"agentd/internal/queue/recovery"
	"agentd/internal/queue/safety"
	qw "agentd/internal/queue/worker"
)

type PIDProbe = safety.PIDProbe
type StaticPIDProbe = safety.StaticPIDProbe
type GopsutilProbe = safety.GopsutilProbe

func BootReconcile(ctx context.Context, store models.KanbanStore, probe safety.PIDProbe, sink models.EventSink) error {
	return recovery.BootReconcile(ctx, store, probe, sink)
}

const (
	RebootRecoveryHandoffEventType = recovery.RebootRecoveryHandoffEventType
	HeartbeatReconcileEventType    = recovery.HeartbeatReconcileEventType
)

type Worker = qw.Worker
type WorkerOptions = qw.WorkerOptions

var NewWorker = qw.NewWorker

const DefaultWorkerMaxRetries = qw.DefaultMaxRetries

type HealingAction = planning.HealingAction

const (
	HealingActionTune  = planning.HealingActionTune
	HealingActionSplit = planning.HealingActionSplit
	HealingActionHuman = planning.HealingActionHuman
)

func IsPhasePlanningTask(title string) bool {
	return planning.IsPhasePlanningTask(title)
}

func NextPhaseNumber(title string) int {
	return planning.NextPhaseNumber(title)
}

func RetitlePhaseContinuationTasks(tasks []models.DraftTask, phase int) []models.DraftTask {
	return planning.RetitlePhaseContinuationTasks(tasks, phase)
}

const (
	HealingStepLowerTemperature = planning.HealingStepLowerTemperature
	HealingStepIncreaseContext  = planning.HealingStepIncreaseContext
	HealingStepCompressContext  = planning.HealingStepCompressContext
	HealingStepUpgradeModel     = planning.HealingStepUpgradeModel
	HealingStepSplitTask        = planning.HealingStepSplitTask
	HealingStepHumanHandoff     = planning.HealingStepHumanHandoff
)

var BuildSandboxEnv = qw.BuildSandboxEnv

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

// DefaultBreakerTimeout matches the circuit breaker open-state timeout.
const DefaultBreakerTimeout = safety.DefaultBreakerTimeout

func NewSemaphore(limit int) *safety.Semaphore { return safety.NewSemaphore(limit) }

type Semaphore = safety.Semaphore

type ParameterTuner = planning.ParameterTuner

var NewParameterTuner = planning.NewParameterTuner
