package testutil

import (
	"context"
	"sort"

	"agentd/internal/models"
)

// The methods below complete FakeKanbanStore's satisfaction of
// models.KanbanBoardContract, so cross-package tests (the MCP board export
// among them) can exercise the paginated, filter-aware board paths against
// the in-memory fake without importing internal/kanban.
var _ models.KanbanBoardContract = (*FakeKanbanStore)(nil)

// ListProjectsPage implements the paginated project listing half of
// models.KanbanBoardContract.
func (s *FakeKanbanStore) ListProjectsPage(_ context.Context, params models.PaginationParams) (models.PaginatedResult[models.Project], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := make([]models.Project, 0, len(s.projects))
	for _, p := range s.projects {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })
	page := normalizeFakePagination(params)
	total := len(all)
	start := min(page.Offset, total)
	end := min(start+page.Limit, total)
	return models.PaginatedResult[models.Project]{
		Data:    all[start:end],
		Total:   total,
		HasNext: end < total,
	}, nil
}

// ListTasks implements the paginated, filter-aware task listing half of
// models.KanbanBoardContract. It mirrors the real store's filtering
// (project_id, states) and pagination (limit/offset with a default page and
// a max page) so board-export tests exercise the same contract the product
// enforces.
func (s *FakeKanbanStore) ListTasks(_ context.Context, filter models.TaskFilter) (models.PaginatedResult[models.Task], error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var all []models.Task
	for _, t := range s.tasks {
		if filter.ProjectID != nil && *filter.ProjectID != "" && t.ProjectID != *filter.ProjectID {
			continue
		}
		if len(filter.States) > 0 {
			matched := false
			for _, st := range filter.States {
				if t.State == st {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		all = append(all, t)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].CreatedAt.Before(all[j].CreatedAt) })
	page := normalizeFakePagination(filter.Pagination)
	total := len(all)
	start := min(page.Offset, total)
	end := min(start+page.Limit, total)
	return models.PaginatedResult[models.Task]{
		Data:    all[start:end],
		Total:   total,
		HasNext: end < total,
	}, nil
}

// AddCommentAndPause implements models.KanbanBoardContract by delegating to
// AddComment; the fake has no pause/HITL side effect to mirror.
func (s *FakeKanbanStore) AddCommentAndPause(ctx context.Context, taskID string, c models.Comment) error {
	c.TaskID = taskID
	if string(c.Author) == "" {
		c.Author = models.CommentAuthorUser
	}
	return s.AddComment(ctx, c)
}

// normalizeFakePagination mirrors the real store's page-size defaults and cap
// (internal/kanban/list_paginated.go) so the fake's paging matches.
func normalizeFakePagination(params models.PaginationParams) models.PaginationParams {
	const (
		defaultPageSize = 25
		maxPageSize     = 200
	)
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
	return normalized
}
