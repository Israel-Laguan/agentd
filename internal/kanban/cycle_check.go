package kanban

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// ensureNoCycle verifies that inserting the edge (parentID -> childID) does not
// create a cycle in the task dependency graph. It walks ancestors of parentID
// via BFS; if childID is reachable from parentID the edge would form a loop.
func ensureNoCycle(ctx context.Context, q sqlQueryer, parentID, childID string) error {
	if parentID == childID {
		return fmt.Errorf("%w: self-referencing edge %s", models.ErrCircularDependency, parentID)
	}
	visited := map[string]struct{}{parentID: {}}
	queue := []string{parentID}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		ancestors, err := parentIDs(ctx, q, current)
		if err != nil {
			return err
		}
		for _, ancestor := range ancestors {
			if ancestor == childID {
				return fmt.Errorf("%w: %s is an ancestor of %s", models.ErrCircularDependency, childID, parentID)
			}
			if _, seen := visited[ancestor]; !seen {
				visited[ancestor] = struct{}{}
				queue = append(queue, ancestor)
			}
		}
	}
	return nil
}

func parentIDs(ctx context.Context, q sqlQueryer, taskID string) ([]string, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT parent_task_id FROM task_relations WHERE child_task_id = ?`, taskID)
	if err != nil {
		return nil, fmt.Errorf("cycle check parent lookup: %w", err)
	}
	defer closeRows(rows)
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("cycle check scan parent: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
