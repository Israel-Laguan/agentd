package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
)

func TestChatCompletionReturnsScopeClarification(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.scope = &gateway.ScopeAnalysis{
		SingleScope: false,
		Confidence:  0.91,
		Scopes: []gateway.ScopeOption{
			{ID: "backend-api", Label: "Backend API service"},
			{ID: "frontend-ui", Label: "Frontend UI"},
		},
		Reason: "distinct deliverables",
	}
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"model":"agentd","messages":[{"role":"user","content":"Build backend API and frontend UI"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	if gw.planCalls != 0 {
		t.Fatalf("GeneratePlan call count = %d, want 0", gw.planCalls)
	}

	content := decodeBody(t, resp)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("clarification content is not JSON: %v", err)
	}
	if parsed["kind"] != "scope_clarification" {
		t.Fatalf("kind = %v", parsed["kind"])
	}
	scopes, ok := parsed["scopes"].([]any)
	if !ok || len(scopes) != 2 {
		t.Fatalf("scopes = %#v", parsed["scopes"])
	}
}

func TestChatCompletionApprovedScopeSkipsAnalyzer(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"messages":[{"role":"user","content":"Build backend API and frontend UI"}],"approved_scopes":["backend-api"]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	if gw.analyzeCalls != 0 {
		t.Fatalf("AnalyzeScope call count = %d, want 0", gw.analyzeCalls)
	}
	if !strings.Contains(gw.lastPlanIntent, "Restrict planning to scope: backend-api") {
		t.Fatalf("GeneratePlan intent missing scope restriction: %q", gw.lastPlanIntent)
	}
}

func TestChatCompletionRejectsMultipleApprovedScopes(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})
	body := `{"messages":[{"role":"user","content":"Build backend API and frontend UI"}],"approved_scopes":["backend-api","frontend-ui"]}`

	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusBadRequest)
	assertNestedField(t, resp, "error", "code", "BAD_REQUEST")
}

func TestChatCompletionSingleScopeAnalyzerGeneratesPlan(t *testing.T) {
	gw := newAPIGateway()
	gw.scope = &gateway.ScopeAnalysis{
		SingleScope: true,
		Confidence:  0.88,
		Scopes:      []gateway.ScopeOption{{ID: "python-scraper", Label: "Python scraper"}},
		Reason:      "cohesive request",
	}
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})
	body := `{"messages":[{"role":"user","content":"A Python script to scrape a website"}]}`

	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	if gw.analyzeCalls != 1 {
		t.Fatalf("AnalyzeScope call count = %d, want 1", gw.analyzeCalls)
	}
	if gw.planCalls != 1 {
		t.Fatalf("GeneratePlan call count = %d, want 1", gw.planCalls)
	}
	content := decodeBody(t, resp)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	if !strings.Contains(content, "Python scraper") {
		t.Fatalf("content missing DraftPlan: %s", content)
	}
}
