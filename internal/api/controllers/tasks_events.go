package controllers

import (
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

const (
	defaultTaskEventLimit = 100
	maxTaskEventLimit     = 200
	maxTaskEventPayload   = 16 * 1024
)

var taskEventScrubber = sandbox.NewScrubber(nil)

type taskEventResponse struct {
	ID               string           `json:"id"`
	ProjectID        string           `json:"project_id"`
	TaskID           string           `json:"task_id,omitempty"`
	Type             models.EventType `json:"type"`
	Payload          string           `json:"payload"`
	PayloadTruncated bool             `json:"payload_truncated,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
}

func (h TaskHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "task store is not configured")
		return
	}
	taskID := r.PathValue("id")
	if _, err := h.Store.GetTask(r.Context(), taskID); err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	limit, err := parseTaskEventLimit(r)
	if err != nil {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation,
			"invalid event query parameters", []string{err.Error()})
		return
	}
	events, err := h.Store.ListEventsByTask(r.Context(), taskID)
	if err != nil {
		httpx.WriteMappedError(w, err)
		return
	}
	total := len(events)
	start := 0
	if total > limit {
		start = total - limit
	}
	selected := events[start:]
	out := make([]taskEventResponse, 0, len(selected))
	for _, event := range selected {
		out = append(out, representTaskEvent(event))
	}
	hasMore := start > 0
	httpx.WriteSuccess(w, http.StatusOK, out, &httpx.Meta{
		Page: 1, PerPage: len(out), Total: total, HasMore: hasMore, Truncated: hasMore,
	})
}

func parseTaskEventLimit(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return defaultTaskEventLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxTaskEventLimit {
		return 0, &invalidTaskEventLimitError{}
	}
	return limit, nil
}

type invalidTaskEventLimitError struct{}

func (*invalidTaskEventLimitError) Error() string {
	return "limit must be an integer between 1 and 200"
}

func representTaskEvent(event models.Event) taskEventResponse {
	payload, truncated := boundedEventPayload(taskEventScrubber.Scrub(event.Payload))
	taskID := ""
	if event.TaskID.Valid {
		taskID = event.TaskID.String
	}
	return taskEventResponse{
		ID: event.ID, ProjectID: event.ProjectID, TaskID: taskID, Type: event.Type,
		Payload: payload, PayloadTruncated: truncated,
		CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
	}
}

func boundedEventPayload(payload string) (string, bool) {
	if len(payload) <= maxTaskEventPayload {
		return payload, false
	}
	limit := maxTaskEventPayload
	for limit > 0 && !utf8.ValidString(payload[:limit]) {
		limit--
	}
	return payload[:limit], true
}
