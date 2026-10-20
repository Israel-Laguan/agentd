package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
	"agentd/internal/services"
)

type usageSource struct {
	got models.AIUsageQuery
	err error
}

func (u *usageSource) AIUsageReport(_ context.Context, q models.AIUsageQuery) (models.AIUsageReport, error) {
	u.got = q
	return models.AIUsageReport{
		GroupBy: q.GroupBy,
		Rows:    []models.AIUsageRow{{Group: map[string]string{"purpose": "exec.turn"}, Calls: 2, Tokens: 90, RepairRate: 0.5}},
		Totals:  models.AIUsageRow{Calls: 2, Tokens: 90},
	}, u.err
}

func getUsage(t *testing.T, h controllers.ReportsHandler, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/ai-usage"+query, nil)
	rec := httptest.NewRecorder()
	h.AIUsageReport(rec, req)
	return rec
}

func usageErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var env struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Error.Code
}

func TestAIUsageReturnsTheReportAndPassesFiltersThrough(t *testing.T) {
	src := &usageSource{}
	h := controllers.ReportsHandler{AIUsage: services.NewAIUsageService(src, nil)}
	rec := getUsage(t, h, "?group_by=day,purpose&from=2026-10-18&to=2026-10-19&task_id=t1&request=req-1&model=m&stage=exec")
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body %s", rec.Code, rec.Body)
	}
	q := src.got
	if len(q.GroupBy) != 2 || q.GroupBy[0] != "day" || q.TaskID != "t1" || q.CorrelationID != "req-1" || q.Model != "m" || q.Stage != "exec" {
		t.Fatalf("query = %+v", q)
	}
	if !q.From.Equal(time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("from = %v", q.From)
	}
	// A date-only "to" includes that whole day.
	if !q.To.Equal(time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("to = %v, want the end of 2026-10-19", q.To)
	}
	var env struct {
		Status string `json:"status"`
		Data   struct {
			Rows   []struct{ Calls, Tokens int } `json:"rows"`
			Totals struct{ Tokens int }          `json:"totals"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Status != "success" || len(env.Data.Rows) != 1 || env.Data.Totals.Tokens != 90 {
		t.Fatalf("envelope = %+v", env)
	}
}

func TestAIUsageErrorCodes(t *testing.T) {
	ok := services.NewAIUsageService(&usageSource{}, nil)
	cases := []struct {
		name   string
		h      controllers.ReportsHandler
		query  string
		status int
		code   string
	}{
		{"unknown group_by", controllers.ReportsHandler{AIUsage: ok}, "?group_by=colour", http.StatusBadRequest, "VALIDATION_FAILED"},
		{"bad from", controllers.ReportsHandler{AIUsage: ok}, "?from=yesterday", http.StatusBadRequest, "VALIDATION_FAILED"},
		{"to before from", controllers.ReportsHandler{AIUsage: ok}, "?from=2026-10-19&to=2026-10-18", http.StatusBadRequest, "VALIDATION_FAILED"},
		{"no service", controllers.ReportsHandler{}, "", http.StatusServiceUnavailable, "UNAVAILABLE"},
		{"no store", controllers.ReportsHandler{AIUsage: services.NewAIUsageService(nil, nil)}, "", http.StatusServiceUnavailable, "UNAVAILABLE"},
		{"store fails", controllers.ReportsHandler{AIUsage: services.NewAIUsageService(&usageSource{err: context.DeadlineExceeded}, nil)}, "", http.StatusInternalServerError, "INTERNAL_ERROR"},
	}
	for _, c := range cases {
		rec := getUsage(t, c.h, c.query)
		if rec.Code != c.status || usageErrorCode(t, rec) != c.code {
			t.Errorf("%s: got %d %s, want %d %s", c.name, rec.Code, rec.Body.String(), c.status, c.code)
		}
	}
}
