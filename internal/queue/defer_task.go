package queue

import (
	"context"
	"log/slog"
	"time"

	"agentd/internal/models"
)

// scheduleTaskDefer registers a future requeue for a task left in QUEUED.
// Uses the scheduler registry when enabled; otherwise falls back to a timer goroutine.
func (d *Daemon) scheduleTaskDefer(ctx context.Context, task models.Task, wait time.Duration, timerDefer func(context.Context, models.Task)) {
	if wait <= 0 {
		wait = time.Minute
	}
	if d.scheduler != nil && d.scheduler.Enabled() {
		runAfter := time.Now().UTC().Add(wait)
		if err := d.scheduler.ScheduleDeferred(ctx, task.ID, runAfter); err != nil {
			slog.Error("schedule deferred requeue failed", "task_id", task.ID, "error", err)
		}
		return
	}
	if timerDefer != nil {
		timerDefer(ctx, task)
	}
}
