package kanban

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

func (s *Store) ListScheduledTasks(ctx context.Context) ([]models.ScheduledTask, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, cron_expr, run_after, task_type, context_fn, context_args, output_target,
		       title, description_template, project_id, kind, target_task_id, last_fired_at,
		       enabled, created_at, updated_at
		FROM scheduled_tasks
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list scheduled tasks: %w", err)
	}
	defer closeRows(rows)
	var out []models.ScheduledTask
	for rows.Next() {
		t, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate scheduled tasks: %w", err)
	}
	return out, nil
}

func (s *Store) UpsertScheduledTask(ctx context.Context, t models.ScheduledTask) error {
	if strings.TrimSpace(t.ID) == "" {
		return fmt.Errorf("%w: scheduled task id is required", models.ErrInvalidDraftPlan)
	}
	if !t.Kind.Valid() {
		t.Kind = models.ScheduledTaskKindDispatch
	}
	argsJSON, err := encodeContextArgs(t.ContextArgs)
	if err != nil {
		return err
	}
	now := utcNow()
	if t.CreatedAt.IsZero() {
		t.CreatedAt = now
	}
	t.UpdatedAt = now
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO scheduled_tasks (
			id, cron_expr, run_after, task_type, context_fn, context_args, output_target,
			title, description_template, project_id, kind, target_task_id, last_fired_at,
			enabled, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			cron_expr = excluded.cron_expr,
			run_after = excluded.run_after,
			task_type = excluded.task_type,
			context_fn = excluded.context_fn,
			context_args = excluded.context_args,
			output_target = excluded.output_target,
			title = excluded.title,
			description_template = excluded.description_template,
			project_id = excluded.project_id,
			kind = excluded.kind,
			target_task_id = excluded.target_task_id,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		t.ID, t.CronExpr, formatOptionalTime(t.RunAfter), t.TaskType, t.ContextFn, argsJSON, t.OutputTarget,
		t.Title, t.DescriptionTemplate, t.ProjectID, string(t.Kind), t.TargetTaskID, formatOptionalTime(t.LastFiredAt),
		scheduledBoolToInt(t.Enabled), formatTime(t.CreatedAt), formatTime(t.UpdatedAt))
	if err != nil {
		return fmt.Errorf("upsert scheduled task %q: %w", t.ID, err)
	}
	return nil
}

func (s *Store) DeleteScheduledTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete scheduled task %q: %w", id, err)
	}
	return nil
}

func (s *Store) UpdateScheduledTaskLastFired(ctx context.Context, id string, firedAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE scheduled_tasks SET last_fired_at = ?, updated_at = ? WHERE id = ?`,
		formatTime(firedAt), formatTime(utcNow()), id)
	if err != nil {
		return fmt.Errorf("update scheduled task last_fired %q: %w", id, err)
	}
	return nil
}

func (s *Store) ScheduleDeferredRequeue(ctx context.Context, taskID string, runAfter time.Time) error {
	id := "defer:" + taskID
	if err := s.DeleteScheduledTask(ctx, id); err != nil {
		return fmt.Errorf("reset deferred scheduled task %q: %w", id, err)
	}
	return s.UpsertScheduledTask(ctx, models.ScheduledTask{
		ID:           id,
		RunAfter:     &runAfter,
		Kind:         models.ScheduledTaskKindRequeue,
		TargetTaskID: taskID,
		Enabled:      true,
	})
}

func (s *Store) InsertReadyTask(ctx context.Context, projectID string, draft models.DraftTask) (*models.Task, error) {
	return s.insertReadyTaskInTx(ctx, projectID, draft, "", time.Time{}, false)
}

func (s *Store) InsertReadyTaskAndRecordDispatch(
	ctx context.Context,
	projectID string,
	draft models.DraftTask,
	scheduleID string,
	slot time.Time,
	deleteEntry bool,
) (*models.Task, error) {
	return s.insertReadyTaskInTx(ctx, projectID, draft, scheduleID, slot, deleteEntry)
}

func (s *Store) insertReadyTaskInTx(
	ctx context.Context,
	projectID string,
	draft models.DraftTask,
	scheduleID string,
	slot time.Time,
	deleteEntry bool,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin insert ready task: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		if _, err := selectProjectByID(ctx, tx, projectID); err != nil {
			return nil, err
		}
		title := strings.TrimSpace(draft.Title)
		if title == "" {
			return nil, fmt.Errorf("%w: task title is required", models.ErrInvalidDraftPlan)
		}
		now := utcNow()
		task := appendedTask(projectID, draft, now)
		task.ID = uuid.NewString()
		task.State = models.TaskStateReady
		if strings.TrimSpace(task.Description) == "" {
			task.Description = title
		}
		if err := insertTask(ctx, tx, task.Title, task); err != nil {
			return nil, err
		}
		if scheduleID != "" {
			res, err := tx.ExecContext(ctx, `
				UPDATE scheduled_tasks SET last_fired_at = ?, updated_at = ? WHERE id = ?`,
				formatTime(slot), formatTime(now), scheduleID)
			if err != nil {
				return nil, fmt.Errorf("update scheduled task last_fired %q: %w", scheduleID, err)
			}
			if err := requireRowsAffected(res, 1, fmt.Errorf("scheduled task %q not found", scheduleID)); err != nil {
				return nil, fmt.Errorf("update scheduled task last_fired %q: %w", scheduleID, err)
			}
			if deleteEntry {
				delRes, err := tx.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE id = ?`, scheduleID)
				if err != nil {
					return nil, fmt.Errorf("delete scheduled task %q: %w", scheduleID, err)
				}
				if err := requireRowsAffected(delRes, 1, fmt.Errorf("scheduled task %q not found", scheduleID)); err != nil {
					return nil, fmt.Errorf("delete scheduled task %q: %w", scheduleID, err)
				}
			}
		}
		return &task, commitTx(tx, "insert ready task")
	})
}

type scheduledTaskScanner interface {
	Scan(dest ...any) error
}

func scanScheduledTask(row scheduledTaskScanner) (models.ScheduledTask, error) {
	var (
		t               models.ScheduledTask
		runAfter        sql.NullString
		lastFired       sql.NullString
		contextArgsJSON string
		kind            string
		enabled         int
		createdAt       string
		updatedAt       string
		err             error
	)
	if err := row.Scan(
		&t.ID, &t.CronExpr, &runAfter, &t.TaskType, &t.ContextFn, &contextArgsJSON, &t.OutputTarget,
		&t.Title, &t.DescriptionTemplate, &t.ProjectID, &kind, &t.TargetTaskID, &lastFired,
		&enabled, &createdAt, &updatedAt,
	); err != nil {
		return models.ScheduledTask{}, fmt.Errorf("scan scheduled task: %w", err)
	}
	t.Kind = models.ScheduledTaskKind(kind)
	t.Enabled = enabled != 0
	t.ContextArgs, err = decodeContextArgs(contextArgsJSON)
	if err != nil {
		return models.ScheduledTask{}, fmt.Errorf("scan scheduled task context args: %w", err)
	}
	if runAfter.Valid && runAfter.String != "" {
		parsed, err := time.Parse(time.RFC3339Nano, runAfter.String)
		if err != nil {
			return models.ScheduledTask{}, fmt.Errorf("scan scheduled task run_after: %w", err)
		}
		t.RunAfter = &parsed
	}
	if lastFired.Valid && lastFired.String != "" {
		parsed, err := time.Parse(time.RFC3339Nano, lastFired.String)
		if err != nil {
			return models.ScheduledTask{}, fmt.Errorf("scan scheduled task last_fired_at: %w", err)
		}
		t.LastFiredAt = &parsed
	}
	t.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return models.ScheduledTask{}, fmt.Errorf("scan scheduled task created_at: %w", err)
	}
	t.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return models.ScheduledTask{}, fmt.Errorf("scan scheduled task updated_at: %w", err)
	}
	return t, nil
}

func encodeContextArgs(args map[string]string) (string, error) {
	if args == nil {
		return "{}", nil
	}
	data, err := json.Marshal(args)
	if err != nil {
		return "", fmt.Errorf("encode context args: %w", err)
	}
	return string(data), nil
}

func decodeContextArgs(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decode context args: %w", err)
	}
	return out, nil
}

func formatOptionalTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return formatTime(*t)
}

func scheduledBoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
