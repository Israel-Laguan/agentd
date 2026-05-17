package kanban

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"agentd/internal/models"
)

type taskScanValues struct {
	task            *models.Task
	createdAt       string
	updatedAt       string
	state           string
	assignee        string
	startedAt       sql.NullString
	completedAt     sql.NullString
	lastHeartbeat   sql.NullString
	osPID           sql.NullInt64
	successCriteria string
}

func scanTaskValues(row scanner, values *taskScanValues) error {
	t := values.task
	err := row.Scan(
		&t.ID, &t.ProjectID, &t.AgentID, &t.Title, &t.Description, &values.state, &values.assignee,
		&values.osPID, &values.startedAt, &values.completedAt, &values.lastHeartbeat, &t.RetryCount, &t.TokenUsage,
		&values.successCriteria, &values.createdAt, &values.updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrTaskNotFound
	}
	if err != nil {
		return fmt.Errorf("scan task: %w", err)
	}
	return nil
}

func (v *taskScanValues) apply() error {
	v.task.State = models.TaskState(v.state)
	v.task.Assignee = models.TaskAssignee(v.assignee)
	applyProcessID(v.task, v.osPID)
	if err := applyOptionalTime(v.startedAt, &v.task.StartedAt); err != nil {
		return err
	}
	if err := applyOptionalTime(v.completedAt, &v.task.CompletedAt); err != nil {
		return err
	}
	if err := applyOptionalTime(v.lastHeartbeat, &v.task.LastHeartbeat); err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(v.successCriteria), &v.task.SuccessCriteria); err != nil {
		return fmt.Errorf("decode success criteria: %w", err)
	}
	return applyEntityTimes(v.task, v.createdAt, v.updatedAt)
}

func applyProcessID(task *models.Task, osPID sql.NullInt64) {
	if osPID.Valid {
		val := int(osPID.Int64)
		task.OSProcessID = &val
	}
}

func applyOptionalTime(source sql.NullString, target **time.Time) error {
	if !source.Valid {
		return nil
	}
	ts, err := parseTime(source.String)
	if err != nil {
		return err
	}
	*target = &ts
	return nil
}

func applyEntityTimes(task *models.Task, createdAt, updatedAt string) error {
	created, err := parseTime(createdAt)
	if err != nil {
		return err
	}
	updated, err := parseTime(updatedAt)
	if err != nil {
		return err
	}
	task.CreatedAt = created
	task.UpdatedAt = updated
	return nil
}
