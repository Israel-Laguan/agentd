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

type routeUsageSource struct{}

func (routeUsageSource) AIUsageReport(_ context.Context, q models.AIUsageQuery) (models.AIUsageReport, error) {
	return models.AIUsageReport{GroupBy: q.GroupBy, Rows: []models.AIUsageRow{}}, nil
}

func getReport(t *testing.T, deps server.ServerDeps) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reports/ai-usage", nil)
	req.Host = "localhost:8765"
	rec := httptest.NewRecorder()
	server.NewHandler(deps).ServeHTTP(rec, req)
	return rec
}

func TestAIUsageRouteServesTheReport(t *testing.T) {
	rec := getReport(t, server.ServerDeps{
		Store: testutil.NewFakeStore(), AIUsage: services.NewAIUsageService(routeUsageSource{}, nil),
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"group_by":["purpose"]`) {
		t.Fatalf("got %d %s, want 200 grouped by purpose", rec.Code, rec.Body)
	}
}

func TestAIUsageRouteIsUnavailableWithoutARecordingStore(t *testing.T) {
	rec := getReport(t, server.ServerDeps{Store: testutil.NewFakeStore()})
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"UNAVAILABLE"`) {
		t.Fatalf("got %d %s, want 503 UNAVAILABLE", rec.Code, rec.Body)
	}
}
