package kanban

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"

	"github.com/google/uuid"
)

func (s *Store) AppendTasksToProject(
	ctx context.Context,
	projectID string,
	parentTaskID string,
	drafts []models.DraftTask,
) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin append tasks: %w", err)
		}
		defer rollbackUnlessCommitted(tx)
		if _, err := selectTaskByID(ctx, tx, parentTaskID); err != nil {
			return nil, err
		}
		tasks, err := appendDraftTasks(ctx, tx, projectID, drafts)
		if err != nil {
			return nil, err
		}
		if err := linkChildrenToParent(ctx, tx, parentTaskID, tasks); err != nil {
			return nil, err
		}
		return tasks, commitTx(tx, "append tasks to project")
	})
}

func appendDraftTasks(
	ctx context.Context,
	tx *immediateTx,
	projectID string,
	drafts []models.DraftTask,
) ([]models.Task, error) {
	now := utcNow()
	tasks := make([]models.Task, 0, len(drafts))
	for i, draft := range drafts {
		task := appendedTask(projectID, draft, now)
		if strings.TrimSpace(task.Title) == "" {
			task.Title = fmt.Sprintf("Follow-up %d", i+1)
		}
		if err := insertTask(ctx, tx, task.Title, task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func appendedTask(projectID string, draft models.DraftTask, nowTime time.Time) models.Task {
	assignee := draft.Assignee
	if !assignee.Valid() {
		assignee = models.TaskAssigneeSystem
	}
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: uuid.NewString(), CreatedAt: nowTime, UpdatedAt: nowTime},
		ProjectID:   projectID,
		AgentID:     defaultAgentID,
		Title:       strings.TrimSpace(draft.Title),
		Description: strings.TrimSpace(draft.Description),
		State:       models.TaskStatePending,
		Assignee:    assignee,
	}
}

func linkChildrenToParent(ctx context.Context, tx *immediateTx, parentID string, tasks []models.Task) error {
	for _, task := range tasks {
		if err := insertTaskRelationChecked(ctx, tx, parentID, task.ID); err != nil {
			return err
		}
	}
	return nil
}
