package kanban

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"
)

// SpawnTieredContinuation inserts child tasks onto an already-BLOCKED tiered
// pipeline origin. Unlike PersistTieredDAG, it never touches the origin
// task's own row: the origin is already BLOCKED for the whole pipeline's
// duration by the time mid-fix, escalation, or a NEEDS_CONTEXT re-gather
// step needs to spawn a continuation.
func (s *Store) SpawnTieredContinuation(
	ctx context.Context,
	originID string,
	children []models.TieredContinuationTask,
) ([]models.Task, error) {
	if len(children) == 0 {
		return nil, models.ErrInvalidDraftPlan
	}
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin spawn tiered continuation: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		if err := s.reblockOrigin(ctx, tx, originID); err != nil {
			return nil, err
		}
		for _, child := range children {
			if child.IdempotencyKey == "" {
				continue
			}
			var childIDs string
			if err := tx.QueryRowContext(ctx, `SELECT child_ids FROM tiered_continuation_keys WHERE origin_id = ? AND idempotency_key = ?`, originID, child.IdempotencyKey).Scan(&childIDs); err == nil {
				return loadTasksByIDs(ctx, tx, childIDs)
			} else if err != sql.ErrNoRows {
				return nil, fmt.Errorf("look up tiered continuation key: %w", err)
			}
		}
		if err := validateTieredChildren(ctx, tx, children); err != nil {
			return nil, err
		}
		tasks := make([]models.Task, 0, len(children))
		for _, child := range children {
			if err := insertTask(ctx, tx, child.Task.Title, child.Task); err != nil {
				return nil, err
			}
			if err := insertTaskRelationWithType(ctx, tx, originID, child.Task.ID, models.TaskRelationSpawnedBy); err != nil {
				return nil, err
			}
			if child.DependsOnID != "" {
				if err := insertTaskRelationWithType(ctx, tx, child.DependsOnID, child.Task.ID, models.TaskRelationDependsOn); err != nil {
					return nil, err
				}
			}
			tasks = append(tasks, child.Task)
		}
		if err := s.insertIdempotencyKeys(ctx, tx, originID, children, tasks); err != nil {
			return nil, err
		}
		return tasks, commitTx(tx, "spawn tiered continuation")
	})
}

func loadTasksByIDs(ctx context.Context, tx *immediateTx, childIDs string) ([]models.Task, error) {
	tasks := make([]models.Task, 0)
	for _, id := range strings.Split(childIDs, ",") {
		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *task)
	}
	return tasks, nil
}

func (s *Store) reblockOrigin(ctx context.Context, tx *immediateTx, originID string) error {
	origin, err := selectTaskByID(ctx, tx, originID)
	if err != nil {
		return err
	}
	if origin.State == models.TaskStateReady {
		now := utcNow()
		result, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND updated_at = ? AND state = ?`, models.TaskStateBlocked, formatTime(now), originID, formatTime(origin.UpdatedAt), models.TaskStateReady)
		if err != nil {
			return fmt.Errorf("re-block tiered continuation origin: %w", err)
		}
		return requireRowsAffected(result, 1, models.ErrStateConflict)
	}
	if origin.State != models.TaskStateBlocked {
		return fmt.Errorf("tiered continuation origin %s is %s, want BLOCKED", originID, origin.State)
	}
	return nil
}

func (s *Store) insertIdempotencyKeys(ctx context.Context, tx *immediateTx, originID string, children []models.TieredContinuationTask, tasks []models.Task) error {
	for _, child := range children {
		if child.IdempotencyKey == "" {
			continue
		}
		ids := make([]string, 0, len(tasks))
		for _, task := range tasks {
			ids = append(ids, task.ID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO tiered_continuation_keys(origin_id, idempotency_key, child_ids) VALUES (?, ?, ?)`, originID, child.IdempotencyKey, strings.Join(ids, ",")); err != nil {
			return err
		}
	}
	return nil
}

func validateTieredChildren(ctx context.Context, tx *immediateTx, children []models.TieredContinuationTask) error {
	seen := make(map[string]struct{}, len(children))
	for _, child := range children {
		if _, duplicate := seen[child.Task.ID]; duplicate {
			return fmt.Errorf("duplicate child ID in tiered continuation: %s", child.Task.ID)
		}
		if child.Task.ID != "" && child.DependsOnID == child.Task.ID {
			return fmt.Errorf("tiered continuation child %s cannot depend on itself", child.Task.ID)
		}
		seen[child.Task.ID] = struct{}{}
		if child.DependsOnID != "" && child.Task.State == models.TaskStateReady {
			return fmt.Errorf("dependent continuation child %s cannot be READY", child.Task.ID)
		}
		if child.DependsOnID != "" {
			if _, isSibling := seen[child.DependsOnID]; !isSibling {
				if _, err := selectTaskByID(ctx, tx, child.DependsOnID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RewireDependsOn redirects unfinished dependents of oldParentID onto
// newParentID. See the KanbanStore interface doc for the READY→BLOCKED
// safety transition and why QUEUED/RUNNING dependents are left untouched.
func (s *Store) RewireDependsOn(ctx context.Context, oldParentID, newParentID string) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin rewire depends_on: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		if _, err := selectTaskByID(ctx, tx, newParentID); err != nil {
			return nil, err
		}

		dependents, err := selectDependsOnChildren(ctx, tx, oldParentID)
		if err != nil {
			return nil, err
		}

		now := utcNow()
		rewired, err := rewireDependents(ctx, tx, dependents, oldParentID, newParentID, now)
		if err != nil {
			return nil, err
		}
		return rewired, commitTx(tx, "rewire depends_on")
	})
}

func rewireDependents(ctx context.Context, tx *immediateTx, dependents []models.Task, oldParentID, newParentID string, now time.Time) ([]models.Task, error) {
	rewired := make([]models.Task, 0, len(dependents))
	for _, dep := range dependents {
		if dep.State != models.TaskStatePending && dep.State != models.TaskStateReady && dep.State != models.TaskStateBlocked {
			continue
		}
		if err := deleteTaskRelation(ctx, tx, oldParentID, dep.ID, models.TaskRelationDependsOn); err != nil {
			return nil, fmt.Errorf("remove stale depends_on edge: %w", err)
		}
		// Make the new edge insertion idempotent: remove any
		// existing edge to newParentID first so that a dependent
		// already depending on newParentID does not hit the
		// composite primary key.
		if err := deleteTaskRelation(ctx, tx, newParentID, dep.ID, models.TaskRelationDependsOn); err != nil {
			return nil, fmt.Errorf("remove existing depends_on edge: %w", err)
		}
		if err := insertTaskRelationWithType(ctx, tx, newParentID, dep.ID, models.TaskRelationDependsOn); err != nil {
			return nil, err
		}
		if dep.State == models.TaskStateReady {
			if err := blockStaleReadyDependent(ctx, tx, dep.ID, dep.UpdatedAt, now); err != nil {
				return nil, err
			}
			dep.State = models.TaskStateBlocked
			dep.UpdatedAt = now
		}
		rewired = append(rewired, dep)
	}
	return rewired, nil
}

func deleteTaskRelation(ctx context.Context, tx *immediateTx, parentID, childID string, relationType models.TaskRelationType) error {
	_, err := tx.ExecContext(ctx, `
		DELETE FROM task_relations
		WHERE parent_task_id = ? AND child_task_id = ? AND relation_type = ?`,
		parentID, childID, string(relationType))
	return err
}

func selectDependsOnChildren(ctx context.Context, tx *immediateTx, parentID string) ([]models.Task, error) {
	rows, err := tx.QueryContext(ctx, taskSelectColumns("tasks")+`
		FROM tasks
		INNER JOIN task_relations tr ON tr.child_task_id = tasks.id
		WHERE tr.parent_task_id = ? AND tr.relation_type = ?
		ORDER BY tasks.created_at`, parentID, string(models.TaskRelationDependsOn))
	if err != nil {
		return nil, fmt.Errorf("select depends_on children: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTasks(rows)
}

func blockStaleReadyDependent(ctx context.Context, tx *immediateTx, id string, expectedUpdatedAt time.Time, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, updated_at = ?
		WHERE id = ? AND updated_at = ? AND state = ?`,
		models.TaskStateBlocked, formatTime(now), id, formatTime(expectedUpdatedAt), models.TaskStateReady)
	if err != nil {
		return fmt.Errorf("block stale ready dependent: %w", err)
	}
	return requireRowsAffected(result, 1, models.ErrStateConflict)
}
