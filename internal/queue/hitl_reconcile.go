package queue

import (
	"context"
	"log/slog"
	"time"
)

func (d *Daemon) hitlTimeoutLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		wait := d.nextHITLReconcileDelay(time.Now())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			logDaemonError("hitl timeout reconcile failed", d.reconcileHITLTimeouts(ctx))
		}
	}
}

func (d *Daemon) nextHITLReconcileDelay(now time.Time) time.Duration {
	if d.hitlReconcileEvery > 0 {
		return d.hitlReconcileEvery
	}
	if d.hitlReconcileSchedule == nil {
		return time.Minute
	}
	next := d.hitlReconcileSchedule.Next(now)
	if !next.After(now) {
		return time.Minute
	}
	return next.Sub(now)
}

func (d *Daemon) reconcileHITLTimeouts(ctx context.Context) error {
	expired, err := d.store.ReconcileExpiredBlockedTasks(ctx, time.Now())
	if err != nil {
		return err
	}
	for _, task := range expired {
		slog.Warn("hitl request timed out", "task_id", task.ID)
	}
	return nil
}
