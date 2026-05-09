package kanban

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

func insertTask(ctx context.Context, tx sqlExecutor, tempID string, task models.Task) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO tasks (
			id, project_id, agent_id, title, description, state, assignee,
			os_process_id, started_at, completed_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.ProjectID, task.AgentID, task.Title, task.Description, string(task.State), string(task.Assignee),
		nil, nil, nil, nil, task.RetryCount, task.TokenUsage, formatTime(task.CreatedAt), formatTime(task.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert task %q: %w", tempID, err)
	}
	return nil
}

func insertRelations(ctx context.Context, tx sqlExecutor, plan models.DraftPlan, taskIDs map[string]string) error {
	for _, draft := range plan.Tasks {
		if err := insertTaskRelations(ctx, tx, draft, taskIDs); err != nil {
			return err
		}
	}
	return nil
}

func insertTaskRelations(
	ctx context.Context,
	tx sqlExecutor,
	draft models.DraftTask,
	taskIDs map[string]string,
) error {
	childID := taskIDs[draft.ID()]
	for _, parentTempID := range draft.DependsOn {
		if err := insertTaskRelation(ctx, tx, taskIDs[parentTempID], childID); err != nil {
			return err
		}
	}
	return nil
}

func insertTaskRelation(ctx context.Context, tx sqlExecutor, parentID, childID string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO task_relations (parent_task_id, child_task_id, relation_type)
		VALUES (?, ?, ?)`, parentID, childID, models.TaskRelationBlocks)
	if err != nil {
		return fmt.Errorf("insert task relation: %w", err)
	}
	return nil
}

// insertTaskRelationChecked inserts a dependency edge and verifies the
// resulting graph remains acyclic. Use this for dynamic edge inserts
// (subtask breakdown, follow-up appends) where the full DAG was not
// pre-validated by MaterializePlan.
func insertTaskRelationChecked(ctx context.Context, tx *immediateTx, parentID, childID string) error {
	if err := ensureNoCycle(ctx, tx, parentID, childID); err != nil {
		return err
	}
	return insertTaskRelation(ctx, tx, parentID, childID)
}
