package queue

import (
	"context"
	"log/slog"
	"time"

	"agentd/internal/config"
	"agentd/internal/models"
)

// deferRollingBudget leaves a task in QUEUED and requeues to READY after the
// estimated window drain. When the scheduler is enabled, run_after replaces the timer.
func (d *Daemon) deferRollingBudget(ctx context.Context, task models.Task, wait time.Duration) {
	d.scheduleTaskDefer(ctx, task, wait, func(ctx context.Context, t models.Task) {
		d.deferRollingBudgetTimer(ctx, t, wait)
	})
}

func (d *Daemon) deferRollingBudgetTimer(ctx context.Context, task models.Task, wait time.Duration) {
	if wait <= 0 {
		wait = time.Minute
	}
	taskID := task.ID
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			current, err := d.store.GetTask(ctx, taskID)
			if err != nil {
				slog.Error("rolling budget defer: get task failed", "task_id", taskID, "error", err)
				return
			}
			if current.State != models.TaskStateQueued {
				return
			}
			if _, err := d.store.UpdateTaskState(ctx, taskID, current.UpdatedAt, models.TaskStateReady); err != nil {
				slog.Error("rolling budget defer: requeue failed", "task_id", taskID, "error", err)
			}
		}
	}()
	slog.Debug("dispatch deferred rolling token budget", "task_id", task.ID, "requeue_after", wait)
}

// projectedTokensForTask estimates tokens for ShouldQueue when profile MaxTokens is unset.
func projectedTokensForTask(profile *models.AgentProfile) int {
	if profile != nil && profile.MaxTokens > 0 {
		return profile.MaxTokens
	}
	return config.DefaultRollingProjectedTokens
}
