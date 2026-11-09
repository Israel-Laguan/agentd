package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agentd/internal/kanban/db"
	"agentd/internal/models"
	"github.com/google/uuid"
)

// UpdateTaskCriteria atomically changes criteria only while the task is not
// running and resets the previous check-cycle history.
func (s *Store) UpdateTaskCriteria(ctx context.Context, id string, expected time.Time, criteria []string, golden []models.GoldenCriterion) (*models.Task, error) {
	if err := models.ValidateGoldenCriteria(criteria, golden); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrInvalidDraftPlan, err)
	}
	if criteria == nil {
		criteria = []string{}
	}
	if golden == nil {
		golden = []models.GoldenCriterion{}
	}
	criteriaJSON, err := json.Marshal(criteria)
	if err != nil {
		return nil, err
	}
	goldenJSON, err := json.Marshal(golden)
	if err != nil {
		return nil, err
	}
	return s.notifyTask(retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		return s.updateTaskCriteriaTx(ctx, id, expected, criteriaJSON, goldenJSON, len(golden) > 0)
	}))
}

func (s *Store) updateTaskCriteriaTx(ctx context.Context, id string, expected time.Time, criteriaJSON, goldenJSON []byte, hasGolden bool) (*models.Task, error) {
	tx, err := beginImmediate(ctx, s.db)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := selectTaskByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if !current.UpdatedAt.Equal(expected) {
		return nil, models.ErrStateConflict
	}
	if current.State == models.TaskStateRunning {
		return nil, models.ErrStateConflict
	}
	if current.State == models.TaskStateCompleted && hasGolden {
		return nil, models.ErrStateConflict
	}
	now := utcNow()
	if err := execUpdateTaskCriteria(tx, ctx, id, criteriaJSON, goldenJSON, now, expected); err != nil {
		return nil, err
	}
	if err := insertCriteriaResetEvent(tx, ctx, current.ProjectID, id, now); err != nil {
		return nil, err
	}
	task, err := selectTaskByID(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return task, commitTx(tx, "update task criteria")
}

func execUpdateTaskCriteria(tx db.SQLExecutor, ctx context.Context, id string, criteriaJSON, goldenJSON []byte, now, expected time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE tasks SET success_criteria=?, golden_criteria=?, criteria_met='[]', updated_at=? WHERE id=? AND updated_at=?`, string(criteriaJSON), string(goldenJSON), formatTime(now), id, formatTime(expected))
	if err != nil {
		return err
	}
	return requireRowsAffected(result, 1, models.ErrStateConflict)
}

func insertCriteriaResetEvent(tx db.SQLExecutor, ctx context.Context, projectID, id string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at) VALUES (?, ?, ?, 'GOLDEN_CRITERIA_RESET', '', ?, ?)`, uuid.NewString(), projectID, id, formatTime(now), formatTime(now))
	return err
}
