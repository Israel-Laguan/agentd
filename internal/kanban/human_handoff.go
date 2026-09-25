package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

const maxHumanResolutionResult = 16 * 1024

type humanResolutionPayload struct {
	ParentTaskID string `json:"parent_task_id"`
	Result       string `json:"result"`
}

func (s *Store) ResolveHumanHandoff(
	ctx context.Context,
	taskID string,
	expectedUpdatedAt *time.Time,
	result string,
) (*models.HumanHandoffResolution, error) {
	result = strings.TrimSpace(sandbox.NewScrubber(nil).Scrub(result))
	if result == "" {
		slog.Warn("resolve human handoff: result is empty", "task_id", taskID)
		return nil, fmt.Errorf("%w: human result is required", models.ErrHumanHandoffInvalid)
	}
	if len(result) > maxHumanResolutionResult {
		slog.Warn("resolve human handoff: result exceeds max bytes", "task_id", taskID, "bytes", len(result), "max", maxHumanResolutionResult)
		return nil, fmt.Errorf("%w: human result exceeds %d bytes", models.ErrHumanHandoffInvalid, maxHumanResolutionResult)
	}
	return retryOnBusy(ctx, func(ctx context.Context) (*models.HumanHandoffResolution, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			slog.Error("resolve human handoff: begin transaction failed", "task_id", taskID, "error", err)
			return nil, fmt.Errorf("begin human handoff resolution: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		child, parent, err := loadHandoffForResolution(ctx, tx, taskID, expectedUpdatedAt)
		if err != nil {
			slog.Warn("resolve human handoff: load handoff failed", "task_id", taskID, "error", err)
			return nil, err
		}

		now := utcNow()
		if err := completeHumanHandoffTask(ctx, tx, *child, now); err != nil {
			return nil, err
		}
		if err := completeHumanHandoffParent(ctx, tx, parent, now); err != nil {
			return nil, err
		}
		if err := finishTaskResultSideEffects(ctx, tx, parent.ID, models.TaskResult{Success: true}, now); err != nil {
			return nil, err
		}
		if err := insertHumanResolutionEvent(ctx, tx, *child, parent.ID, result, now); err != nil {
			return nil, err
		}
		if err := insertHumanResultEvent(ctx, tx, parent, result, now.Add(time.Nanosecond)); err != nil {
			return nil, err
		}

		resolvedChild, err := selectTaskByID(ctx, tx, child.ID)
		if err != nil {
			slog.Error("resolve human handoff: reload child task failed", "task_id", child.ID, "error", err)
			return nil, err
		}
		resolvedParent, err := selectTaskByID(ctx, tx, parent.ID)
		if err != nil {
			slog.Error("resolve human handoff: reload parent task failed", "parent_id", parent.ID, "error", err)
			return nil, err
		}
		slog.Info("human handoff resolved", "child_task_id", child.ID, "parent_task_id", parent.ID, "project_id", child.ProjectID)
		return &models.HumanHandoffResolution{
			Task: resolvedChild, Parent: resolvedParent, Result: result,
		}, commitTx(tx, "human handoff resolution")
	})
}

func loadHandoffForResolution(
	ctx context.Context,
	tx *immediateTx,
	taskID string,
	expectedUpdatedAt *time.Time,
) (*models.Task, models.Task, error) {
	child, err := selectTaskByID(ctx, tx, taskID)
	if err != nil {
		return nil, models.Task{}, err
	}
	if err := validateHandoffChild(child, expectedUpdatedAt); err != nil {
		return nil, models.Task{}, err
	}
	parents, err := selectHandoffParents(ctx, tx, child.ID)
	if err != nil {
		return nil, models.Task{}, err
	}
	if len(parents) != 1 {
		return nil, models.Task{}, fmt.Errorf("%w: human handoff must have exactly one parent", models.ErrHumanHandoffInvalid)
	}
	parent := parents[0]
	if err := validateHandoffParent(ctx, tx, *child, parent); err != nil {
		return nil, models.Task{}, err
	}
	return child, parent, nil
}

func validateHandoffChild(child *models.Task, expectedUpdatedAt *time.Time) error {
	if expectedUpdatedAt != nil && !child.UpdatedAt.Equal(*expectedUpdatedAt) {
		slog.Warn("validate handoff child: optimistic lock conflict", "task_id", child.ID)
		return models.ErrOptimisticLock
	}
	if !models.IsHITLSubtaskTitle(child.Title) || (child.Assignee != models.TaskAssigneeHuman && child.State != models.TaskStateFailedRequiresHuman) {
		slog.Warn("validate handoff child: not an open human handoff", "task_id", child.ID, "assignee", child.Assignee, "state", child.State)
		return fmt.Errorf("%w: task is not an open human handoff", models.ErrHumanHandoffInvalid)
	}
	if child.State == models.TaskStateCompleted || child.State == models.TaskStateFailed {
		slog.Warn("validate handoff child: handoff already resolved", "task_id", child.ID, "state", child.State)
		return fmt.Errorf("%w: handoff is already resolved", models.ErrStateConflict)
	}
	return nil
}

func validateHandoffParent(ctx context.Context, tx *immediateTx, child models.Task, parent models.Task) error {
	parentAllowed := parent.State == models.TaskStateBlocked
	timedOut := parent.State == models.TaskStateFailedRequiresHuman && child.State == models.TaskStateFailedRequiresHuman
	if !parentAllowed && !timedOut {
		slog.Warn("validate handoff parent: parent state invalid for handoff resolution", "parent_id", parent.ID, "parent_state", parent.State, "child_id", child.ID)
		return fmt.Errorf("%w: parent state %s cannot be resolved by human handoff", models.ErrHumanHandoffInvalid, parent.State)
	}
	openSiblings, err := countOpenHandoffSiblings(ctx, tx, parent.ID, child.ID)
	if err != nil {
		slog.Error("validate handoff parent: count open siblings failed", "parent_id", parent.ID, "child_id", child.ID, "error", err)
		return err
	}
	if openSiblings > 0 {
		slog.Warn("validate handoff parent: parent has other open children", "parent_id", parent.ID, "child_id", child.ID, "open_siblings", openSiblings)
		return fmt.Errorf("%w: parent has %d other open children", models.ErrStateConflict, openSiblings)
	}
	return nil
}

func selectHandoffParents(ctx context.Context, tx *immediateTx, childID string) ([]models.Task, error) {
	rows, err := tx.QueryContext(ctx, selectTaskSQL()+`
		INNER JOIN task_relations tr ON tr.parent_task_id = tasks.id
		WHERE tr.child_task_id = ? ORDER BY tasks.created_at`, childID)
	if err != nil {
		return nil, fmt.Errorf("select handoff parent: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

func countOpenHandoffSiblings(ctx context.Context, tx *immediateTx, parentID, childID string) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM task_relations tr
		JOIN tasks child ON child.id = tr.child_task_id
		WHERE tr.parent_task_id = ? AND tr.child_task_id <> ?
		  AND child.state NOT IN (?, ?, ?)`, parentID, childID,
		models.TaskStateCompleted, models.TaskStateFailed, models.TaskStateFailedRequiresHuman).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count open handoff siblings: %w", err)
	}
	return count, nil
}

func completeHumanHandoffTask(ctx context.Context, tx *immediateTx, task models.Task, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND updated_at = ? AND state NOT IN (?, ?)`,
		models.TaskStateCompleted, formatTime(now), formatTime(now), task.ID, formatTime(task.UpdatedAt),
		models.TaskStateCompleted, models.TaskStateFailed)
	if err != nil {
		return fmt.Errorf("complete human handoff task: %w", err)
	}
	return requireRowsAffected(result, 1, models.ErrStateConflict)
}

func completeHumanHandoffParent(ctx context.Context, tx *immediateTx, parent models.Task, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, completed_at = ?, updated_at = ?
		WHERE id = ? AND updated_at = ? AND state IN (?, ?)`,
		models.TaskStateCompleted, formatTime(now), formatTime(now), parent.ID, formatTime(parent.UpdatedAt),
		models.TaskStateBlocked, models.TaskStateFailedRequiresHuman)
	if err != nil {
		return fmt.Errorf("complete human handoff parent: %w", err)
	}
	return requireRowsAffected(result, 1, models.ErrStateConflict)
}

func insertHumanResolutionEvent(ctx context.Context, tx *immediateTx, child models.Task, parentID, result string, now time.Time) error {
	payload, err := json.Marshal(humanResolutionPayload{ParentTaskID: parentID, Result: result})
	if err != nil {
		return fmt.Errorf("encode human resolution event: %w", err)
	}
	return appendTaskEvent(ctx, tx, child.ProjectID, child.ID, models.EventTypeHumanResolution, string(payload), now)
}

func insertHumanResultEvent(ctx context.Context, tx *immediateTx, parent models.Task, result string, now time.Time) error {
	return appendTaskEvent(ctx, tx, parent.ProjectID, parent.ID, models.EventTypeResult, result, now)
}

func appendTaskEvent(ctx context.Context, tx *immediateTx, projectID, taskID string, eventType models.EventType, payload string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), projectID, taskID, eventType, payload, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("append %s event: %w", eventType, err)
	}
	return nil
}
