package queue

import (
	"context"
	"time"
)

func (d *Daemon) dreamLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		wait := d.nextDreamDelay(time.Now())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			logDaemonError("dream agent failed", d.runDream(ctx))
		}
	}
}

func (d *Daemon) nextDreamDelay(now time.Time) time.Duration {
	if d.dreamEvery > 0 {
		return d.dreamEvery
	}
	if d.dreamSchedule == nil {
		return 24 * time.Hour
	}
	next := d.dreamSchedule.Next(now)
	if !next.After(now) {
		return time.Minute
	}
	return next.Sub(now)
}

func (d *Daemon) runDream(ctx context.Context) error {
	if d.dreamer == nil {
		return nil
	}
	return d.dreamer.Run(ctx)
}
