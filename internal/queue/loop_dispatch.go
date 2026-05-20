package queue

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"runtime/debug"

	"agentd/internal/models"
)

func (d *Daemon) dispatch(ctx context.Context) (dispatched int, nacked int, err error) {
	available := d.dispatchAvailable(ctx)
	if available <= 0 {
		return 0, 0, nil
	}
	tasks, err := d.store.ClaimNextReadyTasks(ctx, available)
	if err != nil {
		return 0, 0, err
	}
	for i, task := range tasks {
		nack, skip := d.dispatchAdmit(ctx, task)
		if nack {
			nacked++
		}
		if skip {
			continue
		}
		if d.dispatchDeferRollingBudget(ctx, task) {
			continue
		}
		if !d.sem.Acquire(ctx) {
			d.requeueUndispatchedClaims(context.WithoutCancel(ctx), tasks[i:])
			return dispatched, nacked, nil
		}
		dispatched++
		d.runDispatchedTask(ctx, task)
	}
	return dispatched, nacked, nil
}

func (d *Daemon) requeueUndispatchedClaims(ctx context.Context, tasks []models.Task) {
	for _, task := range tasks {
		d.requeueClaimedTask(ctx, task)
	}
}

// failDispatchPanic marks a task FAILED after a panic in the dispatch goroutine.
func (d *Daemon) failDispatchPanic(ctx context.Context, task models.Task, panicMsg string) {
	current, err := d.store.GetTask(ctx, task.ID)
	if err != nil {
		slog.Error("dispatch panic: get task failed", "task_id", task.ID, "error", err)
		return
	}
	if current.State != models.TaskStateRunning && current.State != models.TaskStateQueued {
		return
	}
	if _, updateErr := d.store.UpdateTaskState(ctx, task.ID, current.UpdatedAt, models.TaskStateFailed); updateErr != nil {
		slog.Error("dispatch panic: failed to mark task failed", "task_id", task.ID, "error", updateErr)
		return
	}
	if d.sink != nil {
		emitErr := d.sink.Emit(ctx, models.Event{
			ProjectID: task.ProjectID,
			TaskID:    sql.NullString{String: task.ID, Valid: true},
			Type:      models.EventTypeFailure,
			Payload:   "dispatch panic: " + panicMsg,
		})
		if emitErr != nil {
			slog.Error("dispatch panic: failed to emit event", "task_id", task.ID, "error", emitErr)
		}
	}
}

func (d *Daemon) requeueClaimedTask(ctx context.Context, task models.Task) {
	current, err := d.store.GetTask(ctx, task.ID)
	if err != nil {
		slog.Error("requeue claimed task: get task failed", "task_id", task.ID, "error", err)
		return
	}
	if current.State != models.TaskStateQueued {
		return
	}
	if _, err := d.store.UpdateTaskState(ctx, task.ID, current.UpdatedAt, models.TaskStateReady); err != nil {
		slog.Error("requeue claimed task: update state failed", "task_id", task.ID, "error", err)
	}
}

func (d *Daemon) dispatchAvailable(ctx context.Context) int {
	available := d.sem.Available()
	if d.breaker != nil {
		available = d.breaker.ProbeLimit(available)
		if available <= 0 && d.breaker.OpenDuration() >= d.handoffAfter {
			logDaemonError("outage handoff failed", d.checkOutageHandoff(ctx))
		}
	}
	return available
}

func (d *Daemon) dispatchAdmit(ctx context.Context, task models.Task) (nacked bool, skip bool) {
	if d.channel == nil {
		return false, false
	}
	msg := TaskToInbound(task)
	result := d.channel.Admit(msg)
	if result.Disposition != Nack {
		return false, false
	}
	slog.Warn("dispatch nack", "task_id", task.ID, "error", result.Err)
	if classifyDispatchNack(result.Err) {
		d.deferRateLimited(ctx, task)
	} else {
		d.failDispatchRejected(ctx, task, result.Err)
	}
	return true, true
}

func (d *Daemon) dispatchDeferRollingBudget(ctx context.Context, task models.Task) bool {
	if d.rollingLedger == nil || !d.rollingLedger.Enabled() {
		return false
	}
	profile, err := d.store.GetAgentProfile(ctx, task.AgentID)
	if err != nil {
		return false
	}
	projected := projectedTokensForTask(profile)
	if !d.rollingLedger.ShouldQueue(projected, d.rollingLedger.Limit()) {
		return false
	}
	wait := d.rollingLedger.EstimatedDrainWait(projected)
	d.deferRollingBudget(ctx, task, wait)
	if d.sink != nil {
		_ = d.sink.Emit(ctx, models.Event{
			ProjectID: task.ProjectID,
			TaskID:    sql.NullString{String: task.ID, Valid: true},
			Type:      "ROLLING_BUDGET_DEFER",
			Payload:   fmt.Sprintf("deferred %s for rolling token budget", wait),
		})
	}
	return true
}

func (d *Daemon) runDispatchedTask(ctx context.Context, task models.Task) {
	d.wg.Add(1)
	go func(task models.Task) {
		defer d.wg.Done()
		defer d.sem.Release()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("dispatch goroutine panic", "task_id", task.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				d.failDispatchPanic(context.WithoutCancel(ctx), task, fmt.Sprint(r))
			}
		}()
		runCtx, cancel := context.WithTimeout(ctx, d.taskDeadline)
		defer cancel()
		d.worker.Process(runCtx, task)
	}(task)
}
