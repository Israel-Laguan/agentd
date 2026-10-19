package queue

import (
	"context"
	"time"

	"agentd/internal/config"
)

func (d *Daemon) schedulerLoop(ctx context.Context) {
	defer d.wg.Done()
	if d.scheduler == nil || !d.scheduler.Enabled() {
		return
	}
	interval := d.schedulerTickEvery
	if interval <= 0 {
		interval = config.DefaultSchedulerTickInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			guardTick("scheduler tick failed", func() error { return d.scheduler.Tick(ctx, now) })
		}
	}
}
