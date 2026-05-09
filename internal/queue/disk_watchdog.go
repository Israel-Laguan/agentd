package queue

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
)

const diskHandoffTitle = "Disk space critical. Please run cleanup or expand storage."

func (d *Daemon) diskWatchdogLoop(ctx context.Context) {
	defer d.wg.Done()
	for {
		wait := d.nextDiskWatchdogDelay(time.Now())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			logDaemonError("disk watchdog failed", d.checkDiskSpace(ctx))
		}
	}
}

func (d *Daemon) nextDiskWatchdogDelay(now time.Time) time.Duration {
	if d.diskWatchdogEvery > 0 {
		return d.diskWatchdogEvery
	}
	if d.diskWatchdogSchedule == nil {
		return 10 * time.Minute
	}
	next := d.diskWatchdogSchedule.Next(now)
	if !next.After(now) {
		return time.Minute
	}
	return next.Sub(now)
}

func (d *Daemon) checkDiskSpace(ctx context.Context) error {
	if d.diskStat == nil {
		d.diskStat = safety.DiskFreePercent
	}
	freePercent, err := d.diskStat(d.diskCheckPath)
	if err != nil {
		return err
	}
	if freePercent >= d.diskFreeThreshold {
		return nil
	}
	project, err := d.store.EnsureSystemProject(ctx)
	if err != nil {
		return err
	}
	task, created, err := d.store.EnsureProjectTask(ctx, project.ID, models.DraftTask{
		Title:       diskHandoffTitle,
		Description: d.diskHandoffDescription(freePercent),
		Assignee:    models.TaskAssigneeHuman,
	})
	if err != nil {
		return err
	}
	if !created || d.sink == nil {
		return nil
	}
	return d.sink.Emit(ctx, models.Event{
		ProjectID: project.ID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      "DISK_SPACE_CRITICAL",
		Payload:   d.diskHandoffPayload(freePercent),
	})
}

func (d *Daemon) diskHandoffDescription(freePercent float64) string {
	return fmt.Sprintf(`Disk free space is below the configured threshold.
Path checked: %s
Free space: %.2f%%
Threshold: %.2f%%
Observed at: %s

Suggested actions:
1. Remove unnecessary files from the agentd home or project directories.
2. Archive or rotate large logs and generated artifacts.
3. Expand the filesystem if cleanup is not enough.
4. Mark this task complete once free space is back above the threshold.`,
		d.diskCheckPath,
		freePercent,
		d.diskFreeThreshold,
		time.Now().UTC().Format(time.RFC3339),
	)
}

func (d *Daemon) diskHandoffPayload(freePercent float64) string {
	return fmt.Sprintf("path=%s free_percent=%.2f threshold_percent=%.2f", d.diskCheckPath, freePercent, d.diskFreeThreshold)
}
