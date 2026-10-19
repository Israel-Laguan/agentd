package queue

import (
	"context"
	"time"
)

func (d *Daemon) taskLoop(ctx context.Context) {
	defer d.wg.Done()
	wait := d.taskInterval
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			var dispatched, nacked int
			guardTick("queue dispatch failed", func() (err error) {
				dispatched, nacked, err = d.dispatch(ctx)
				return err
			})
			wait = d.nextDispatchDelay(wait, dispatched, nacked)
			timer.Reset(wait)
		}
	}
}

func (d *Daemon) nextDispatchDelay(prev time.Duration, dispatched, nacked int) time.Duration {
	if dispatched > 0 || nacked > 0 {
		return d.taskInterval
	}
	next := prev * 2
	if next > d.maxTaskInterval {
		next = d.maxTaskInterval
	}
	return next
}

func (d *Daemon) intakeLoop(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(d.intakeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			guardTick("comment intake failed", func() error { return d.processComments(ctx) })
		}
	}
}

func (d *Daemon) heartbeatReconcileLoop(ctx context.Context) {
	defer d.wg.Done()
	ticker := time.NewTicker(d.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			guardTick("heartbeat reconciliation failed", func() error { return d.reconcileHeartbeats(ctx) })
		}
	}
}

func (d *Daemon) reconcileOrphanedQueued(ctx context.Context) error {
	if d.queuedReconcileAfter <= 0 {
		return nil
	}
	_, err := d.store.ReconcileOrphanedQueued(ctx, d.queuedReconcileAfter)
	return err
}

func (d *Daemon) queuedReconcileLoop(ctx context.Context) {
	defer d.wg.Done()
	if d.queuedReconcileAfter <= 0 {
		return
	}
	interval := d.queuedReconcileAfter / 2
	if interval < d.taskInterval {
		interval = d.taskInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			guardTick("orphaned queued reconcile failed", func() error { return d.reconcileOrphanedQueued(ctx) })
		}
	}
}

func (d *Daemon) processComments(ctx context.Context) error {
	if d.intake == nil {
		return nil
	}
	refs, err := d.store.ListUnprocessedHumanComments(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := d.intake.Process(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
