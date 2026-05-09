package kanban

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentd/internal/kanban/domain"
	"agentd/internal/models"

	"github.com/google/uuid"
)

func (s *Store) MaterializePlan(ctx context.Context, plan models.DraftPlan) (*models.Project, []models.Task, error) {
	normalized, err := prepareDraftPlan(plan)
	if err != nil {
		return nil, nil, err
	}

	type result struct {
		project *models.Project
		tasks   []models.Task
	}
	r, err := retryOnBusy(ctx, func(ctx context.Context) (result, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return result{}, fmt.Errorf("begin materialize plan: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		now := utcNow()
		project := newProject(normalized, now)
		taskIDs := newTaskIDs(normalized.Tasks)
		if err := insertProject(ctx, tx, project); err != nil {
			return result{}, err
		}
		tasks, err := insertTasks(ctx, tx, normalized, project.ID, taskIDs, now)
		if err != nil {
			return result{}, err
		}
		if err := insertRelations(ctx, tx, normalized, taskIDs); err != nil {
			return result{}, err
		}
		if err := tx.Commit(); err != nil {
			return result{}, fmt.Errorf("commit materialize plan: %w", err)
		}
		return result{project, tasks}, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return r.project, r.tasks, nil
}

func (s *Store) GetProject(ctx context.Context, id string) (*models.Project, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, original_input, workspace_path, status, created_at, updated_at
		FROM projects WHERE id = ?`, id)
	return scanProject(row)
}

func (s *Store) ListProjects(ctx context.Context) ([]models.Project, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, original_input, workspace_path, status, created_at, updated_at
		FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer closeRows(rows)
	return scanProjects(rows)
}

func prepareDraftPlan(plan models.DraftPlan) (models.DraftPlan, error) {
	normalized, err := domain.NormalizeDraftPlan(plan)
	if err != nil {
		return models.DraftPlan{}, err
	}
	if err := domain.ValidateDAG(normalized); err != nil {
		return models.DraftPlan{}, err
	}
	return normalized, nil
}

func newProject(plan models.DraftPlan, now time.Time) *models.Project {
	projectID := uuid.NewString()
	return &models.Project{
		BaseEntity:    models.BaseEntity{ID: projectID, CreatedAt: now, UpdatedAt: now},
		Name:          strings.TrimSpace(plan.ProjectName),
		OriginalInput: strings.TrimSpace(plan.Description),
		WorkspacePath: projectID,
		Status:        models.ProjectStatusActive,
	}
}

func newTaskIDs(tasks []models.DraftTask) map[string]string {
	taskIDs := make(map[string]string, len(tasks))
	for _, task := range tasks {
		taskIDs[task.ID()] = uuid.NewString()
	}
	return taskIDs
}

func insertProject(ctx context.Context, tx sqlExecutor, project *models.Project) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO projects (id, name, original_input, workspace_path, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		project.ID, project.Name, project.OriginalInput, project.WorkspacePath, project.Status,
		formatTime(project.CreatedAt), formatTime(project.UpdatedAt))
	if err != nil {
		return fmt.Errorf("insert project: %w", err)
	}
	return nil
}

func insertTasks(
	ctx context.Context,
	tx sqlExecutor,
	plan models.DraftPlan,
	projectID string,
	taskIDs map[string]string,
	now time.Time,
) ([]models.Task, error) {
	tasks := make([]models.Task, 0, len(plan.Tasks))
	for _, draft := range plan.Tasks {
		draftID := draft.ID()
		task := newTask(draft, projectID, taskIDs[draftID], now)
		if err := insertTask(ctx, tx, draftID, task); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

func newTask(draft models.DraftTask, projectID, taskID string, now time.Time) models.Task {
	state := models.TaskStateReady
	if len(draft.DependsOn) > 0 {
		state = models.TaskStatePending
	}
	return models.Task{
		BaseEntity:  models.BaseEntity{ID: taskID, CreatedAt: now, UpdatedAt: now},
		ProjectID:   projectID,
		AgentID:     defaultAgentID,
		Title:       strings.TrimSpace(draft.Title),
		Description: strings.TrimSpace(draft.Description),
		State:       state,
		Assignee:    draft.Assignee,
	}
}
