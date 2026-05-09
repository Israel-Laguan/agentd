package models

import "time"

// PaginationParams defines reusable list paging and ordering controls.
type PaginationParams struct {
	Limit  int
	Offset int
	SortBy string
	Order  string
}

// PaginatedResult wraps paged responses from store-layer list operations.
type PaginatedResult[T any] struct {
	Data    []T
	Total   int
	HasNext bool
}

// TaskFilter safely constrains task list queries.
type TaskFilter struct {
	Pagination   PaginationParams
	ProjectID    *string
	States       []TaskState
	Assignee     *TaskAssignee
	UpdatedAfter *time.Time
}
