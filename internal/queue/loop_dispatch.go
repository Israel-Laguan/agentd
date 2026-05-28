package queue

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"agentd/internal/models"
	qw "agentd/internal/queue/worker"
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
	toGroup, nacked := d.dispatchFilterClaimed(ctx, tasks)
	if d.worker == nil {
		return 0, nacked, d.dispatchGuardNilWorker(ctx, toGroup)
	}
	batches := d.worker.GroupClaimed(ctx, toGroup)
	dispatched, err = d.dispatchRunBatches(ctx, batches)
	return dispatched, nacked, err
}

func (d *Daemon) dispatchFilterClaimed(ctx context.Context, tasks []models.Task) (toGroup []models.Task, nacked int) {
	d.refreshRollingLedgerFromStore(ctx)
	for _, task := range tasks {
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
		toGroup = append(toGroup, task)
	}
	return toGroup, nacked
}

// refreshRollingLedgerFromStore reloads the rolling ledger from persisted token
// usage events so queueing decisions are based on durable data, not only process
// memory. Failures are non-fatal and leave the in-memory snapshot in place.
// Throttled to run at most once every 30 seconds to avoid database overload.
func (d *Daemon) refreshRollingLedgerFromStore(ctx context.Context) {
	if d.rollingLedger == nil || !d.rollingLedger.Enabled() {
		return
	}
	if time.Since(d.lastRollingLedgerRefresh) < 30*time.Second {
		return
	}
	src, ok := d.store.(TokenUsageEventSource)
	if !ok {
		return
	}
	if err := d.rollingLedger.HydrateFromStore(ctx, src); err != nil {
		slog.Warn("rolling token ledger refresh failed", "error", err)
		return
	}
	d.lastRollingLedgerRefresh = time.Now()
}

func (d *Daemon) dispatchGuardNilWorker(ctx context.Context, toGroup []models.Task) error {
	if len(toGroup) == 0 {
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	d.requeueUndispatchedClaims(cleanupCtx, toGroup)
	if ctx.Err() != nil {
		return nil
	}
	return fmt.Errorf("dispatch worker is nil")
}

func (d *Daemon) dispatchRunBatches(ctx context.Context, batches []qw.TaskBatch) (dispatched int, err error) {
	for batchIdx, batch := range batches {
		need := len(batch.Tasks)
		acquired := 0
		for acquired < need {
			if !d.sem.Acquire(ctx) {
				for i := 0; i < acquired; i++ {
					d.sem.Release()
				}
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				for _, remaining := range batches[batchIdx:] {
					d.requeueUndispatchedClaims(cleanupCtx, remaining.Tasks)
				}
				return dispatched, nil
			}
			acquired++
		}
		dispatched += need
		if need == 1 {
			d.runDispatchedTask(ctx, batch.Tasks[0], 1)
		} else {
			d.runDispatchedBatch(ctx, batch.Tasks, need)
		}
	}
	return dispatched, nil
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

func (d *Daemon) runDispatchedTask(ctx context.Context, task models.Task, semSlots int) {
	d.wg.Add(1)
	go func(task models.Task, semSlots int) {
		defer d.wg.Done()
		defer func() {
			for i := 0; i < semSlots; i++ {
				d.sem.Release()
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("dispatch goroutine panic", "task_id", task.ID, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				d.failDispatchPanic(cleanupCtx, task, fmt.Sprint(r))
			}
		}()
		runCtx, cancel := context.WithTimeout(ctx, d.taskDeadline)
		defer cancel()
		d.worker.Process(runCtx, task)
	}(task, semSlots)
}

func (d *Daemon) runDispatchedBatch(ctx context.Context, tasks []models.Task, semSlots int) {
	d.wg.Add(1)
	cp := append([]models.Task(nil), tasks...)
	go func(tasks []models.Task, semSlots int) {
		defer d.wg.Done()
		defer func() {
			for i := 0; i < semSlots; i++ {
				d.sem.Release()
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("dispatch batch goroutine panic", "task_count", len(tasks), "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				for _, task := range tasks {
					d.failDispatchPanic(cleanupCtx, task, fmt.Sprint(r))
				}
			}
		}()
		runCtx, cancel := context.WithTimeout(ctx, d.taskDeadline)
		defer cancel()
		d.worker.ProcessBatch(runCtx, tasks)
	}(cp, semSlots)
}
