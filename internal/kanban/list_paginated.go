package kanban

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
)

const (
	defaultPageSize = 25
	maxPageSize     = 200
)

func (s *Store) ListProjectsPage(ctx context.Context, params models.PaginationParams) (models.PaginatedResult[models.Project], error) {
	page := normalizePagination(params, map[string]struct{}{
		"created_at": {},
		"updated_at": {},
		"name":       {},
	})
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&total); err != nil {
		return models.PaginatedResult[models.Project]{}, fmt.Errorf("count projects: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, original_input, workspace_path, status, created_at, updated_at
		FROM projects
		ORDER BY `+page.SortBy+` `+page.Order+`
		LIMIT ? OFFSET ?`, page.Limit, page.Offset)
	if err != nil {
		return models.PaginatedResult[models.Project]{}, fmt.Errorf("list projects page: %w", err)
	}
	defer closeRows(rows)
	projects, err := scanProjects(rows)
	if err != nil {
		return models.PaginatedResult[models.Project]{}, err
	}
	return paginatedResult(projects, total, page), nil
}

func (s *Store) ListTasks(ctx context.Context, filter models.TaskFilter) (models.PaginatedResult[models.Task], error) {
	page := normalizePagination(filter.Pagination, map[string]struct{}{
		"created_at":   {},
		"updated_at":   {},
		"started_at":   {},
		"completed_at": {},
	})
	clauses := make([]string, 0, 4)
	args := make([]any, 0, 8)
	if filter.ProjectID != nil && strings.TrimSpace(*filter.ProjectID) != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, strings.TrimSpace(*filter.ProjectID))
	}
	if filter.Assignee != nil {
		clauses = append(clauses, "assignee = ?")
		args = append(args, *filter.Assignee)
	}
	if filter.UpdatedAfter != nil {
		clauses = append(clauses, "updated_at > ?")
		args = append(args, formatTime(*filter.UpdatedAfter))
	}
	if len(filter.States) > 0 {
		clauses = append(clauses, "state IN ("+placeholders(len(filter.States))+")")
		for _, state := range filter.States {
			args = append(args, state)
		}
	}

	where := ""
	if len(clauses) > 0 {
		where = " WHERE " + strings.Join(clauses, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tasks`+where, args...).Scan(&total); err != nil {
		return models.PaginatedResult[models.Task]{}, fmt.Errorf("count tasks: %w", err)
	}
	queryArgs := append(args, page.Limit, page.Offset)
	rows, err := s.db.QueryContext(ctx, selectTaskSQL()+where+` ORDER BY `+page.SortBy+` `+page.Order+` LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return models.PaginatedResult[models.Task]{}, fmt.Errorf("list tasks page: %w", err)
	}
	defer closeRows(rows)
	tasks, err := scanTasks(rows)
	if err != nil {
		return models.PaginatedResult[models.Task]{}, err
	}
	return paginatedResult(tasks, total, page), nil
}

func normalizePagination(params models.PaginationParams, allowedSort map[string]struct{}) models.PaginationParams {
	normalized := params
	if normalized.Limit <= 0 {
		normalized.Limit = defaultPageSize
	}
	if normalized.Limit > maxPageSize {
		normalized.Limit = maxPageSize
	}
	if normalized.Offset < 0 {
		normalized.Offset = 0
	}
	sortBy := strings.TrimSpace(strings.ToLower(normalized.SortBy))
	if _, ok := allowedSort[sortBy]; !ok {
		sortBy = "created_at"
	}
	normalized.SortBy = sortBy
	order := strings.TrimSpace(strings.ToUpper(normalized.Order))
	if order != "ASC" {
		order = "DESC"
	}
	normalized.Order = order
	return normalized
}

func paginatedResult[T any](data []T, total int, params models.PaginationParams) models.PaginatedResult[T] {
	nextOffset := params.Offset + len(data)
	return models.PaginatedResult[T]{
		Data:    data,
		Total:   total,
		HasNext: nextOffset < total,
	}
}
