package server_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/server"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
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

func (stubGateway) Embed(context.Context, gateway.EmbedRequest) (gateway.EmbedResponse, error) {
	return gateway.EmbedResponse{}, nil
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

func TestNewHandlerRoutesTaskEvents(t *testing.T) {
	store := testutil.NewFakeStore()
	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "events",
		Tasks:       []models.DraftTask{{Title: "Inspect"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if err := store.AppendEvent(context.Background(), models.Event{
		BaseEntity: models.BaseEntity{ID: "event-1"},
		ProjectID:  project.ID,
		TaskID:     sql.NullString{String: tasks[0].ID, Valid: true},
		Type:       models.EventTypeResult,
		Payload:    "done",
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	handler := server.NewHandler(server.ServerDeps{Store: store, Summarizer: frontdesk.NewStatusSummarizer(store)})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/"+tasks[0].ID+"/events", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":"event-1"`) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestAgentCreateWarnsUnknownModelWhenGatewayIsRouter(t *testing.T) {
	store := testutil.NewFakeStore()
	router, err := gateway.NewRouterFromConfigs([]spec.ProviderConfig{
		{Adapter: "openai", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	h := server.NewHandler(server.ServerDeps{
		Store:      store,
		Gateway:    router,
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{"id":"warn-agent","name":"Warn","provider":"openai","model":"gpt-4o"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	warnings, ok := resp["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one model hint", resp["warnings"])
	}
}

func TestAgentCreateRejectsUnknownProviderWhenGatewayIsRouter(t *testing.T) {
	store := testutil.NewFakeStore()
	router, err := gateway.NewRouterFromConfigs([]spec.ProviderConfig{
		{Adapter: "openai", BaseURL: "https://api.openai.com/v1"},
	})
	if err != nil {
		t.Fatalf("NewRouterFromConfigs: %v", err)
	}
	h := server.NewHandler(server.ServerDeps{
		Store:      store,
		Gateway:    router,
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	body := `{"id":"bad-agent","name":"Bad","provider":"poolside","model":"poolside/laguna"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj := resp["error"].(map[string]any)
	if errObj["code"] != "VALIDATION_FAILED" {
		t.Fatalf("error.code = %v, want VALIDATION_FAILED", errObj["code"])
	}
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "poolside") || !strings.Contains(msg, "openai") {
		t.Fatalf("error.message = %q, want poolside rejection listing openai", msg)
	}
}

func TestNewServerUsesHandler(t *testing.T) {
	store := testutil.NewFakeStore()
	srv := &http.Server{
		Addr: "127.0.0.1:0",
		Handler: server.NewHandler(server.ServerDeps{
			Addr:       "127.0.0.1:0",
			Store:      store,
			Gateway:    stubGateway{},
			Bus:        bus.NewInProcess(),
			Summarizer: frontdesk.NewStatusSummarizer(store),
		}),
	}
	if srv.Handler == nil {
		t.Fatal("nil handler")
	}
}
