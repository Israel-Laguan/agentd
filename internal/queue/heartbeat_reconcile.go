package queue

import (
	"context"

	"agentd/internal/queue/recovery"
)

func (d *Daemon) reconcileHeartbeats(ctx context.Context) error {
	alive, err := d.probe.AlivePIDs(ctx)
	if err != nil {
		return err
	}
	recovered, err := d.store.ReconcileStaleTasks(ctx, alive, d.staleAfter)
	if err != nil {
		return err
	}
	return recovery.EmitHeartbeatReconcile(ctx, d.sink, recovered)
}
