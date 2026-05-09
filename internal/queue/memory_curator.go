package queue

import (
	"context"
	"log/slog"
	"time"
)

func (d *Daemon) memoryCuratorLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		wait := d.nextCuratorDelay(time.Now())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			logDaemonError("memory curator failed", d.curateMemories(ctx))
		}
	}
}

func (d *Daemon) nextCuratorDelay(now time.Time) time.Duration {
	if d.curatorEvery > 0 {
		return d.curatorEvery
	}
	if d.curatorSchedule == nil {
		return time.Hour
	}
	next := d.curatorSchedule.Next(now)
	if !next.After(now) {
		return time.Minute
	}
	return next.Sub(now)
}

func (d *Daemon) curateMemories(ctx context.Context) error {
	if d.librarian == nil {
		return nil
	}
	retention := time.Duration(d.librarian.Cfg.RetentionHours) * time.Hour
	tasks, err := d.store.ListCompletedTasksOlderThan(ctx, retention)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := d.librarian.CurateTask(ctx, task); err != nil {
			slog.Error("curate task failed", "task", task.ID, "error", err)
			continue
		}
	}
	purged, err := d.librarian.CleanStaleArchives()
	if err != nil {
		slog.Error("clean stale archives failed", "error", err)
	}
	if len(purged) > 0 {
		if err := d.librarian.PurgeCuratedEvents(ctx, purged); err != nil {
			slog.Error("purge curated events failed", "error", err)
		}
	}
	return nil
}
