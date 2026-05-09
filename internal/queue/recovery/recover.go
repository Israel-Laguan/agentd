package recovery

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
)

// Event type constants reused by integration tests outside this package.
const (
	RebootRecoveryHandoffEventType = models.EventTypeRebootRecoveryHandoff
	HeartbeatReconcileEventType    = models.EventTypeHeartbeatReconcile
)

const rebootRecoveryMemoryScope = models.MemoryScopeGlobal

func BootReconcile(ctx context.Context, store models.KanbanStore, probe safety.PIDProbe, sink models.EventSink) error {
	alive, err := probe.AlivePIDs(ctx)
	if err != nil {
		return err
	}
	recovered, err := store.ReconcileGhostTasks(ctx, alive)
	if err != nil {
		return err
	}
	if err := emitRecoveredTasks(ctx, sink, recovered); err != nil {
		return err
	}
	return reportRebootRecovery(ctx, store, sink, recovered)
}

func EmitHeartbeatReconcile(ctx context.Context, sink models.EventSink, tasks []models.Task) error {
	if sink == nil {
		return nil
	}
	for _, task := range tasks {
		err := sink.Emit(ctx, models.Event{
			ProjectID: task.ProjectID,
			TaskID:    sql.NullString{String: task.ID, Valid: true},
			Type:      HeartbeatReconcileEventType,
			Payload:   "daemon reset stale running task to READY",
		})
		if err != nil {
			return fmt.Errorf("emit heartbeat reconcile event: %w", err)
		}
	}
	return nil
}

func emitRecoveredTasks(ctx context.Context, sink models.EventSink, tasks []models.Task) error {
	if sink == nil {
		return nil
	}
	for _, task := range tasks {
		err := sink.Emit(ctx, models.Event{
			ProjectID: task.ProjectID,
			TaskID:    sql.NullString{String: task.ID, Valid: true},
			Type:      models.EventTypeRecovery,
			Payload:   "daemon reset ghost task to READY",
		})
		if err != nil {
			return fmt.Errorf("emit recovery event: %w", err)
		}
	}
	return nil
}

func reportRebootRecovery(ctx context.Context, store models.KanbanStore, sink models.EventSink, tasks []models.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		return fmt.Errorf("ensure system project for reboot recovery: %w", err)
	}
	payload := rebootRecoveryPayload(tasks, time.Now().UTC())
	if err := store.AppendEvent(ctx, models.Event{
		ProjectID: project.ID,
		Type:      models.EventTypeRebootRecovery,
		Payload:   payload,
	}); err != nil {
		return fmt.Errorf("append reboot recovery event: %w", err)
	}
	task, created, err := store.EnsureProjectTask(ctx, project.ID, models.DraftTask{
		Title:       fmt.Sprintf("Daemon Reboot Recovery: %d task(s) interrupted", len(tasks)),
		Description: rebootRecoveryDescription(tasks, time.Now().UTC()),
		Assignee:    models.TaskAssigneeHuman,
	})
	if err != nil {
		return fmt.Errorf("create reboot recovery review task: %w", err)
	}
	if err := store.RecordMemory(ctx, models.Memory{
		Scope: rebootRecoveryMemoryScope,
		Tags: sql.NullString{
			String: "reboot,recovery",
			Valid:  true,
		},
		Symptom: sql.NullString{
			String: "daemon_reboot_interrupted_tasks",
			Valid:  true,
		},
		Solution: sql.NullString{
			String: "Tasks were reset to READY after daemon restart. These interruptions are caused by host reboot or daemon crash, not inherent task failure. Do not count toward task failure metrics.",
			Valid:  true,
		},
	}); err != nil {
		return fmt.Errorf("record reboot recovery memory: %w", err)
	}
	if !created || sink == nil {
		return nil
	}
	return sink.Emit(ctx, models.Event{
		ProjectID: project.ID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      RebootRecoveryHandoffEventType,
		Payload:   payload,
	})
}

func rebootRecoveryDescription(tasks []models.Task, observedAt time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "agentd recovered %d task(s) that were RUNNING before daemon startup.\n", len(tasks))
	fmt.Fprintf(&b, "Observed at: %s\n\n", observedAt.UTC().Format(time.RFC3339))
	b.WriteString("Recovered tasks:\n")
	for _, task := range tasks {
		fmt.Fprintf(&b, "- %s: %s (project: %s)\n", task.ID, task.Title, task.ProjectID)
	}
	b.WriteString("\nSuggested review actions:\n")
	b.WriteString("1. Verify each recovered task output when it completes.\n")
	b.WriteString("2. Check whether any partial filesystem or repository changes were applied before reboot.\n")
	b.WriteString("3. Mark this task complete after confirming recovery is understood.\n")
	return b.String()
}

func rebootRecoveryPayload(tasks []models.Task, observedAt time.Time) string {
	parts := []string{
		fmt.Sprintf("task_count=%d", len(tasks)),
		fmt.Sprintf("observed_at=%s", observedAt.UTC().Format(time.RFC3339)),
	}
	for _, task := range tasks {
		parts = append(parts, fmt.Sprintf("task=%s project=%s title=%q", task.ID, task.ProjectID, task.Title))
	}
	return strings.Join(parts, " ")
}
