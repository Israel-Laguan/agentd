package kanban

import (
	"database/sql"
	"errors"
	"fmt"

	"agentd/internal/models"
)

type scanner interface{ Scan(dest ...any) error }

func scanProject(row scanner) (*models.Project, error) {
	var p models.Project
	var createdAt, updatedAt string
	err := row.Scan(&p.ID, &p.Name, &p.OriginalInput, &p.WorkspacePath, &p.Status, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, models.ErrProjectNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan project: %w", err)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := parseTime(updatedAt)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = created
	p.UpdatedAt = updated
	return &p, nil
}

func scanProjects(rows *sql.Rows) ([]models.Project, error) {
	var out []models.Project
	for rows.Next() {
		project, err := scanProject(rows)
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

func scanTask(row scanner) (*models.Task, error) {
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

func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var out []models.Task
	for rows.Next() {
		task, err := scanTask(rows)
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
