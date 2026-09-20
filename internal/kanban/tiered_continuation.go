package kanban

import (
	"context"
	"fmt"
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

		origin, err := selectTaskByID(ctx, tx, originID)
		if err != nil {
			return nil, err
		}
		if origin.State == models.TaskStateReady {
			now := utcNow()
			result, err := tx.ExecContext(ctx, `UPDATE tasks SET state = ?, updated_at = ? WHERE id = ? AND updated_at = ? AND state = ?`, models.TaskStateBlocked, formatTime(now), originID, formatTime(origin.UpdatedAt), models.TaskStateReady)
			if err != nil {
				return nil, fmt.Errorf("re-block tiered continuation origin: %w", err)
			}
			if err := requireRowsAffected(result, 1, models.ErrStateConflict); err != nil {
				return nil, err
			}
		} else if origin.State != models.TaskStateBlocked {
			return nil, fmt.Errorf("tiered continuation origin %s is %s, want BLOCKED", originID, origin.State)
		}
		seen := make(map[string]struct{}, len(children))
		for _, child := range children {
			if _, duplicate := seen[child.Task.ID]; duplicate {
				return nil, fmt.Errorf("duplicate child ID in tiered continuation: %s", child.Task.ID)
			}
			if child.Task.ID != "" && child.DependsOnID == child.Task.ID {
				return nil, fmt.Errorf("tiered continuation child %s cannot depend on itself", child.Task.ID)
			}
			seen[child.Task.ID] = struct{}{}
			if child.DependsOnID != "" {
				if _, isSibling := seen[child.DependsOnID]; !isSibling {
					if _, err := selectTaskByID(ctx, tx, child.DependsOnID); err != nil {
						return nil, err
					}
				}
			}
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
		return tasks, commitTx(tx, "spawn tiered continuation")
	})
}

// RewireDependsOn redirects PENDING/READY dependents of oldParentID onto
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
		rewired := make([]models.Task, 0, len(dependents))
		for _, dep := range dependents {
			if dep.State != models.TaskStatePending && dep.State != models.TaskStateReady {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM task_relations
				WHERE parent_task_id = ? AND child_task_id = ? AND relation_type = ?`,
				oldParentID, dep.ID, string(models.TaskRelationDependsOn)); err != nil {
				return nil, fmt.Errorf("remove stale depends_on edge: %w", err)
			}
			// Make the new edge insertion idempotent: remove any
			// existing edge to newParentID first so that a dependent
			// already depending on newParentID does not hit the
			// composite primary key.
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM task_relations
				WHERE parent_task_id = ? AND child_task_id = ? AND relation_type = ?`,
				newParentID, dep.ID, string(models.TaskRelationDependsOn)); err != nil {
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
		return rewired, commitTx(tx, "rewire depends_on")
	})
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
