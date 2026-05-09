package controllers

import (
	"errors"
	"net/http"
	"strings"

	"agentd/internal/models"
)

func parseTaskFilter(r *http.Request) (models.TaskFilter, []string) {
	q := r.URL.Query()
	var (
		filter models.TaskFilter
		errs   []string
	)
	limit, err := parsePositiveInt(q.Get("limit"))
	if err != nil {
		errs = append(errs, "limit must be a non-negative integer")
	}
	offset, err := parsePositiveInt(q.Get("offset"))
	if err != nil {
		errs = append(errs, "offset must be a non-negative integer")
	}
	filter.Pagination = models.PaginationParams{Limit: limit, Offset: offset, SortBy: q.Get("sort_by"), Order: q.Get("order")}

	if raw := strings.TrimSpace(q.Get("state")); raw != "" {
		for _, item := range strings.Split(raw, ",") {
			state := models.TaskState(strings.ToUpper(strings.TrimSpace(item)))
			if !state.Valid() {
				errs = append(errs, "unknown task state: "+item)
				continue
			}
			filter.States = append(filter.States, state)
		}
	}
	if raw := strings.TrimSpace(q.Get("assignee")); raw != "" {
		assignee := models.TaskAssignee(strings.ToUpper(raw))
		if !assignee.Valid() {
			errs = append(errs, "unknown assignee: "+raw)
		} else {
			filter.Assignee = &assignee
		}
	}
	return filter, errs
}

func parsePositiveInt(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	var n int
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, errors.New("non-digit character")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
