package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

// PersistTieredDAG atomically blocks the parent task, inserts the pre-built
// child tasks, and creates both SPAWNED_BY and DEPENDS_ON relations.
// The parent must be in RUNNING or READY state. The first child in children
// should be READY (no dependency); subsequent children should be PENDING with
// DependsOnID pointing to their predecessor.
func (s *Store) PersistTieredDAG(
	ctx context.Context,
	parentID string,
	expectedParentUpdatedAt time.Time,
	children []models.TieredDAGTask,
) ([]models.Task, error) {
	if len(children) == 0 {
		return nil, models.ErrInvalidDraftPlan
	}
	type result struct {
		children []models.Task
	}
	r, err := retryOnBusy(ctx, func(ctx context.Context) (result, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return result{}, fmt.Errorf("begin persist tiered dag: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		parent, err := selectTaskByID(ctx, tx, parentID)
		if err != nil {
			return result{}, err
		}
		if parent.State != models.TaskStateRunning && parent.State != models.TaskStateReady {
			return result{}, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, parent.State, models.TaskStateBlocked)
		}

		now := utcNow()
		if err := blockTask(ctx, tx, parent.ID, expectedParentUpdatedAt, now); err != nil {
			return result{}, err
		}

		tasks := make([]models.Task, 0, len(children))
		for _, child := range children {
			if err := insertTask(ctx, tx, child.Task.Title, child.Task); err != nil {
				return result{}, err
			}
			if err := insertTaskRelationWithType(ctx, tx, parentID, child.Task.ID, models.TaskRelationSpawnedBy); err != nil {
				return result{}, err
			}
			if child.DependsOnID != "" {
				if err := insertTaskRelationWithType(ctx, tx, child.DependsOnID, child.Task.ID, models.TaskRelationDependsOn); err != nil {
					return result{}, err
				}
			}
			tasks = append(tasks, child.Task)
		}
		return result{tasks}, commitTx(tx, "persist tiered dag")
	})
	if err != nil {
		return nil, err
	}
	return r.children, nil
}

// insertTaskRelationWithType inserts a task relation row with the given type.
func insertTaskRelationWithType(ctx context.Context, tx *immediateTx, parentID, childID string, relType models.TaskRelationType) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO task_relations (parent_task_id, child_task_id, relation_type)
		VALUES (?, ?, ?)`, parentID, childID, string(relType))
	if err != nil {
		return fmt.Errorf("insert task relation (%s): %w", relType, err)
	}
	return nil
}
