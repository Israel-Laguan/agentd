package queue

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

const outageHandoffTitle = "System Offline: Please check AI API connections."

func (d *Daemon) checkOutageHandoff(ctx context.Context) error {
	if d.breaker == nil || d.breaker.OpenDuration() < d.handoffAfter {
		return nil
	}
	project, err := d.store.EnsureSystemProject(ctx)
	if err != nil {
		return err
	}
	task, created, err := d.store.EnsureProjectTask(ctx, project.ID, models.DraftTask{
		Title:       outageHandoffTitle,
		Description: d.outageHandoffDescription(),
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
		Type:      "LLM_OUTAGE_HANDOFF",
		Payload:   d.outageHandoffPayload(),
	})
}

func (d *Daemon) outageHandoffDescription() string {
	lastErr := "unavailable"
	if err := d.breaker.LastError(); err != nil {
		lastErr = err.Error()
	}
	return fmt.Sprintf(`The AI provider circuit breaker has been open for %s.
Consecutive failures: %d
Last error: %s
Observed at: %s

Suggested actions:
1. Check network connectivity to AI provider endpoints.
2. Verify API keys and quota limits.
3. Check provider status pages.
4. Mark this task complete once connectivity is restored.`,
		d.breaker.OpenDuration().Round(time.Second),
		d.breaker.FailureCount(),
		truncate(lastErr, 500),
		d.breaker.Now().UTC().Format(time.RFC3339),
	)
}

func (d *Daemon) outageHandoffPayload() string {
	parts := []string{
		fmt.Sprintf("open_duration=%s", d.breaker.OpenDuration().Round(time.Second)),
		fmt.Sprintf("failure_count=%d", d.breaker.FailureCount()),
	}
	if err := d.breaker.LastError(); err != nil {
		parts = append(parts, "last_error="+truncate(err.Error(), 500))
	}
	return strings.Join(parts, " ")
}
