package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/server"
	"agentd/internal/models"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

type toolUseSource struct {
	routeUsageSource
	got models.ToolUseQuery
}

func (s *toolUseSource) ToolUseReport(_ context.Context, q models.ToolUseQuery) (models.ToolUseReport, error) {
	s.got = q
	return models.ToolUseReport{Reads: 3, RereadBytes: 40, RepeatedReads: []models.RepeatedRead{{TaskID: "t1", Path: "a.go", Reads: 3}}}, nil
}

func getToolUse(deps server.ServerDeps, query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/tool-use"+query, nil)
	req.Host = "localhost:8765"
	rec := httptest.NewRecorder()
	server.NewHandler(deps).ServeHTTP(rec, req)
	return rec
}

func TestToolUseRouteServesTheReportAndPassesFilters(t *testing.T) {
	src := &toolUseSource{}
	rec := getToolUse(server.ServerDeps{Store: testutil.NewFakeStore(), AIUsage: services.NewAIUsageService(src, nil)},
		"?task_id=t1&from=2026-10-18&to=2026-10-19")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"reread_bytes":40`) {
		t.Fatalf("got %d %s, want 200 with the re-read bytes", rec.Code, rec.Body)
	}
	if src.got.TaskID != "t1" || src.got.From.IsZero() || src.got.To.Day() != 20 {
		t.Fatalf("query = %+v, want task t1 and a date-only to that includes the day", src.got)
	}
}

func TestToolUseRouteIsUnavailableWhenTheStoreKeepsNoToolCalls(t *testing.T) {
	rec := getToolUse(server.ServerDeps{Store: testutil.NewFakeStore(), AIUsage: services.NewAIUsageService(routeUsageSource{}, nil)}, "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d %s, want 503", rec.Code, rec.Body)
	}
}
