package queue

import (
	"context"
	"log/slog"
	"time"
)

func (d *Daemon) hitlTimeoutLoop(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logDaemonError("hitl timeout reconcile failed", d.reconcileHITLTimeouts(ctx))
		}
	}
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
