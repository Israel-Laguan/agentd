package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/server"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type stubGateway struct{}

func (stubGateway) Generate(context.Context, gateway.AIRequest) (gateway.AIResponse, error) {
	return gateway.AIResponse{}, nil
}

func (stubGateway) GeneratePlan(context.Context, string) (*models.DraftPlan, error) {
	return &models.DraftPlan{}, nil
}

func (stubGateway) AnalyzeScope(context.Context, string) (*gateway.ScopeAnalysis, error) {
	return &gateway.ScopeAnalysis{}, nil
}

func (stubGateway) ClassifyIntent(context.Context, string) (*gateway.IntentAnalysis, error) {
	return &gateway.IntentAnalysis{}, nil
}

func TestNewHandlerGETProjectsSmoke(t *testing.T) {
	store := testutil.NewFakeStore()
	h := server.NewHandler(server.ServerDeps{
		Store:      store,
		Gateway:    stubGateway{},
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestNewServerUsesHandler(t *testing.T) {
	store := testutil.NewFakeStore()
	srv := server.NewServer(server.ServerDeps{
		Addr:       "127.0.0.1:0",
		Store:      store,
		Gateway:    stubGateway{},
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	if srv.Handler == nil {
		t.Fatal("nil handler")
	}
}
