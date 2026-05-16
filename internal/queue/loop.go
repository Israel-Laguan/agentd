package queue

import (
	"context"
	"fmt"
	"log/slog"
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
			claimed, err := d.dispatch(ctx)
			logDaemonError("queue dispatch failed", err)
			wait = d.nextDispatchDelay(wait, claimed)
			timer.Reset(wait)
		}
	}
}

func (d *Daemon) nextDispatchDelay(prev time.Duration, claimed int) time.Duration {
	if claimed > 0 {
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
			logDaemonError("comment intake failed", d.processComments(ctx))
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
			logDaemonError("heartbeat reconciliation failed", d.reconcileHeartbeats(ctx))
		}
	}
}

func (d *Daemon) dispatch(ctx context.Context) (int, error) {
	available := d.sem.Available()
	if d.breaker != nil {
		available = d.breaker.ProbeLimit(available)
		if available <= 0 && d.breaker.OpenDuration() >= d.handoffAfter {
			logDaemonError("outage handoff failed", d.checkOutageHandoff(ctx))
		}
	}
	if available <= 0 {
		return 0, nil
	}
	if d.queuedReconcileAfter > 0 {
		_, reconcileErr := d.store.ReconcileOrphanedQueued(ctx, d.queuedReconcileAfter)
		logDaemonError("orphaned queued reconcile failed", reconcileErr)
	}
	tasks, err := d.store.ClaimNextReadyTasks(ctx, available)
	if err != nil {
		return 0, err
	}
	var nacked int
	for _, task := range tasks {
		if d.channel != nil {
			msg := TaskToInbound(task)
			result := d.channel.Admit(msg)
			if result.Disposition == Nack {
				nacked++
				slog.Warn("dispatch nack", "task_id", task.ID, "error", result.Err)
				if classifyDispatchNack(result.Err) {
					d.deferRateLimited(ctx, task)
				} else {
					d.failDispatchRejected(ctx, task, result.Err)
				}
				continue
			}
		}
		if !d.sem.Acquire(ctx) {
			return len(tasks) - nacked, ctx.Err()
		}
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			defer d.sem.Release()
			defer func() {
				if r := recover(); r != nil {
					slog.Error("dispatch goroutine panic", "task_id", task.ID, "panic", fmt.Sprint(r))
				}
			}()
			runCtx, cancel := context.WithTimeout(ctx, d.taskDeadline)
			defer cancel()
			d.worker.Process(runCtx, task)
		}()
	}
	return len(tasks) - nacked, nil
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
