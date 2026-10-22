package queue

import (
	"context"
	"log/slog"
	"time"
)

// usagePruneEvery is how often the daemon deletes usage records past the
// retention window (T-056). Pruning also runs once at start.
const usagePruneEvery = 6 * time.Hour

// usagePruner is the optional store capability the retention loop needs.
type usagePruner interface {
	PruneUsageBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

// pruneUsage deletes usage records older than the retention window. A store
// without the pruner keeps everything, and so does retention 0.
func (d *Daemon) pruneUsage(ctx context.Context, now time.Time) {
	pruner, ok := d.store.(usagePruner)
	if !ok || d.usageRetentionDays <= 0 {
		return
	}
	cutoff := now.AddDate(0, 0, -d.usageRetentionDays)
	removed, err := pruner.PruneUsageBefore(ctx, cutoff)
	if err != nil {
		slog.Warn("ai usage prune failed", "cutoff", cutoff, "error", err)
		return
	}
	if removed > 0 {
		slog.Info("ai usage pruned", "rows", removed, "cutoff", cutoff)
	}
}

func (d *Daemon) usageRetentionLoop(ctx context.Context) {
	defer d.wg.Done()
	d.pruneUsage(ctx, time.Now())
	ticker := time.NewTicker(usagePruneEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			d.pruneUsage(ctx, now)
		}
	}
}
