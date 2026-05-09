package kanban

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"

	"github.com/google/uuid"
)

func (s *Store) BlockTaskWithSubtasks(
	ctx context.Context,
	taskID string,
	expectedUpdatedAt time.Time,
	subtasks []models.DraftTask,
) (*models.Task, []models.Task, error) {
	if len(subtasks) == 0 {
		return nil, nil, models.ErrInvalidDraftPlan
	}
	type result struct {
		blocked  *models.Task
		children []models.Task
	}
	r, err := retryOnBusy(ctx, func(ctx context.Context) (result, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return result{}, fmt.Errorf("begin block task with subtasks: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		parent, err := selectTaskByID(ctx, tx, taskID)
		if err != nil {
			return result{}, err
		}
		if parent.State != models.TaskStateRunning && parent.State != models.TaskStateReady {
			return result{}, fmt.Errorf("%w: %s -> %s", models.ErrInvalidStateTransition, parent.State, models.TaskStateBlocked)
		}

		now := utcNow()
		if err := blockTask(ctx, tx, parent.ID, expectedUpdatedAt, now); err != nil {
			return result{}, err
		}
		children, err := insertReadySubtasks(ctx, tx, parent.ProjectID, parent.ID, subtasks, now)
		if err != nil {
			return result{}, err
		}
		blocked, err := selectTaskByID(ctx, tx, parent.ID)
		if err != nil {
			return result{}, err
		}
		return result{blocked, children}, commitTx(tx, "block task with subtasks")
	})
	if err != nil {
		return nil, nil, err
	}
	return r.blocked, r.children, nil
}

func blockTask(ctx context.Context, tx *immediateTx, taskID string, expectedUpdatedAt time.Time, now time.Time) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, os_process_id = NULL, updated_at = ?
		WHERE id = ? AND updated_at = ? AND state IN (?, ?)`,
		models.TaskStateBlocked, formatTime(now), taskID, formatTime(expectedUpdatedAt),
		models.TaskStateRunning, models.TaskStateReady)
	if err != nil {
		return fmt.Errorf("block task: %w", err)
	}
	return requireRowsAffected(result, 1, models.ErrStateConflict)
}

func insertReadySubtasks(
	ctx context.Context,
	tx *immediateTx,
	projectID string,
	parentID string,
	drafts []models.DraftTask,
	now time.Time,
) ([]models.Task, error) {
	tasks := make([]models.Task, 0, len(drafts))
	for i, draft := range drafts {
		task := readySubtask(projectID, draft, now)
		if strings.TrimSpace(task.Title) == "" {
			task.Title = fmt.Sprintf("Subtask %d", i+1)
		}
		if err := insertTask(ctx, tx, task.Title, task); err != nil {
			return nil, err
		}
		if err := insertTaskRelationChecked(ctx, tx, parentID, task.ID); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func readySubtask(projectID string, draft models.DraftTask, now time.Time) models.Task {
	assignee := draft.Assignee
	if !assignee.Valid() {
		assignee = models.TaskAssigneeSystem
	}
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: now, UpdatedAt: now},
		ProjectID:   projectID,
		AgentID:     defaultAgentID,
		Title:       strings.TrimSpace(draft.Title),
		Description: strings.TrimSpace(draft.Description),
		State:       models.TaskStateReady,
		Assignee:    assignee,
	}
}
