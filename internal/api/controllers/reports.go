package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agentd/internal/api/httpx"
	"agentd/internal/models"
	"agentd/internal/services"
)

// ReportsHandler serves read-only reports built from stored records.
type ReportsHandler struct {
	AIUsage *services.AIUsageService
}

// AIUsageReport handles GET /api/v1/reports/ai-usage.
// Query params (all optional):
//   - group_by: comma list of purpose, stage, model, day, task, request (default purpose)
//   - from, to: YYYY-MM-DD or RFC3339; from is inclusive, to is exclusive, and a
//     date-only to includes that whole day
//   - task_id, request (the chat request's correlation ID), purpose, stage, model: exact filters
func (h ReportsHandler) AIUsageReport(w http.ResponseWriter, r *http.Request) {
	query, err := parseAIUsageQuery(r)
	if err != nil {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation, err.Error(), nil)
		return
	}
	report, err := h.AIUsage.Report(r.Context(), query)
	switch {
	case errors.Is(err, services.ErrAIUsageUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "ai usage report is not available")
	case err != nil:
		httpx.WriteMappedError(w, err)
	default:
		httpx.WriteSuccess(w, http.StatusOK, report, nil)
	}
}

func parseAIUsageQuery(r *http.Request) (models.AIUsageQuery, error) {
	v := r.URL.Query()
	q := models.AIUsageQuery{
		TaskID: v.Get("task_id"), CorrelationID: v.Get("request"),
		Purpose: v.Get("purpose"), Stage: v.Get("stage"), Model: v.Get("model"),
	}
	if g := strings.TrimSpace(v.Get("group_by")); g != "" {
		for _, part := range strings.Split(g, ",") {
			q.GroupBy = append(q.GroupBy, strings.TrimSpace(part))
		}
	}
	var err error
	if q.From, _, err = parseReportTime("from", v.Get("from")); err != nil {
		return q, err
	}
	var dateOnly bool
	if q.To, dateOnly, err = parseReportTime("to", v.Get("to")); err != nil {
		return q, err
	}
	if dateOnly {
		q.To = q.To.AddDate(0, 0, 1)
	}
	return q, nil
}

// parseReportTime reads an empty value as zero, a YYYY-MM-DD as that UTC
// midnight (dateOnly true), and anything else as RFC3339.
func parseReportTime(name, value string) (t time.Time, dateOnly bool, err error) {
	if value == "" {
		return time.Time{}, false, nil
	}
	if t, err = time.Parse("2006-01-02", value); err == nil {
		return t, true, nil
	}
	if t, err = time.Parse(time.RFC3339, value); err == nil {
		return t.UTC(), false, nil
	}
	return time.Time{}, false, fmt.Errorf("%s must be YYYY-MM-DD or RFC3339, got %q", name, value)
}

// ToolUseReport handles GET /api/v1/reports/tool-use: tool calls by tool and
// the files a task read more than once. Query params (all optional): task_id,
// from, to (same rules as the AI usage report).
func (h ReportsHandler) ToolUseReport(w http.ResponseWriter, r *http.Request) {
	v := r.URL.Query()
	q := models.ToolUseQuery{TaskID: v.Get("task_id")}
	var err error
	if q.From, _, err = parseReportTime("from", v.Get("from")); err == nil {
		var dateOnly bool
		if q.To, dateOnly, err = parseReportTime("to", v.Get("to")); err == nil && dateOnly {
			q.To = q.To.AddDate(0, 0, 1)
		}
	}
	if err != nil {
		httpx.WriteValidationError(w, http.StatusBadRequest, httpx.CodeValidation, err.Error(), nil)
		return
	}
	report, err := h.AIUsage.ToolUse(r.Context(), q)
	switch {
	case errors.Is(err, services.ErrAIUsageUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeUnavailable, "tool use report is not available")
	case err != nil:
		httpx.WriteMappedError(w, err)
	default:
		httpx.WriteSuccess(w, http.StatusOK, report, nil)
	}
}
