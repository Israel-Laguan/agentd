package kanban

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"

	"github.com/google/uuid"
)

const (
	systemProjectID            = "00000000-0000-0000-0000-000000000001"
	systemProjectName          = "_system"
	systemProjectWorkspacePath = "_system"
)

func (s *Store) EnsureSystemProject(ctx context.Context) (*models.Project, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Project, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin ensure system project: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		now := formatTime(utcNow())
		_, err = tx.ExecContext(ctx, `
			INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'ACTIVE', ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				workspace_path = excluded.workspace_path,
				status = 'ACTIVE',
				updated_at = excluded.updated_at`,
			systemProjectID, systemProjectName, "System diagnostics and operator action items.",
			systemProjectWorkspacePath, now, now)
		if err != nil {
			return nil, fmt.Errorf("upsert system project: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit ensure system project: %w", err)
		}
		return s.GetProject(ctx, systemProjectID)
	})
}

func (s *Store) EnsureProjectTask(ctx context.Context, projectID string, draft models.DraftTask) (*models.Task, bool, error) {
	type result struct {
		task    *models.Task
		created bool
	}
	r, err := retryOnBusy(ctx, func(ctx context.Context) (result, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return result{}, fmt.Errorf("begin ensure project task: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		if _, err := selectProjectByID(ctx, tx, projectID); err != nil {
			return result{}, err
		}
		title := strings.TrimSpace(draft.Title)
		if title == "" {
			return result{}, fmt.Errorf("%w: task title is required", models.ErrInvalidDraftPlan)
		}
		existing, found, err := selectOpenTaskByTitle(ctx, tx, projectID, title, draft.Assignee)
		if err != nil {
			return result{}, err
		}
		if found {
			return result{existing, false}, commitTx(tx, "ensure existing project task")
		}

		now := utcNow()
		task := appendedTask(projectID, draft, now)
		task.ID = uuid.NewString()
		task.State = models.TaskStateReady
		if strings.TrimSpace(task.Description) == "" {
			task.Description = title
		}
		if err := insertTask(ctx, tx, task.Title, task); err != nil {
			return result{}, err
		}
		return result{&task, true}, commitTx(tx, "ensure project task")
	})
	if err != nil {
		return nil, false, err
	}
	return r.task, r.created, nil
}

func selectProjectByID(ctx context.Context, tx sqlExecutor, id string) (*models.Project, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, name, original_input, workspace_path, status, created_at, updated_at
		FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

func selectOpenTaskByTitle(
	ctx context.Context,
	tx sqlExecutor,
	projectID string,
	title string,
	assignee models.TaskAssignee,
) (*models.Task, bool, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, project_id, agent_id, title, description, state, assignee, os_process_id,
		       started_at, completed_at, last_heartbeat, retry_count, token_usage, created_at, updated_at
		FROM tasks
		WHERE project_id = ? AND title = ? AND assignee = ?
		  AND state NOT IN (?, ?)
		ORDER BY created_at LIMIT 1`,
		projectID, title, assignee, models.TaskStateCompleted, models.TaskStateFailed)
	task, err := scanTask(row)
	if err != nil {
		if err == models.ErrTaskNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	return task, true, nil
}
