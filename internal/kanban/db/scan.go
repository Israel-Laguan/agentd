package db

import (
	"database/sql"
	"errors"
	"fmt"

	"agentd/internal/models"
)

// Scanner is the minimal interface satisfied by *sql.Row and *sql.Rows.
type Scanner interface{ Scan(dest ...any) error }

// ScanProject scans one row into a *models.Project.
func ScanProject(row Scanner) (*models.Project, error) {
	var p models.Project
	var createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.OriginalInput, &p.WorkspacePath, &p.Status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	created, err := ParseTime(createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := ParseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = created
	p.UpdatedAt = updated
	return &p, nil
}

// ScanProjects scans all rows from a project query result.
func ScanProjects(rows *sql.Rows) ([]models.Project, error) {
	var out []models.Project
	for rows.Next() {
		project, err := ScanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	return out, nil
}

// ScanTask scans one row into a *models.Task.
func ScanTask(row Scanner) (*models.Task, error) {
	var t models.Task
	values := taskScanValues{task: &t}
	if err := scanTaskValues(row, &values); err != nil {
		return nil, err
	}
	if err := values.apply(); err != nil {
		return nil, err
	}
	return &t, nil
}

// ScanTasks scans all rows from a task query result.
func ScanTasks(rows *sql.Rows) ([]models.Task, error) {
	var out []models.Task
	for rows.Next() {
		task, err := ScanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}
	return out, nil
}
