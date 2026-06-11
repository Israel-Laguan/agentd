package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/gateway"
)

func TestStatusCheckReturnsReportAndSkipsPlanning(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intent = &gateway.IntentAnalysis{Intent: "status_check", Reason: "user asked for status"}
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"messages":[{"role":"user","content":"What's the status of my projects?"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	content := decodeBody(t, resp)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("status content is not JSON: %v", err)
	}
	if parsed["kind"] != "status_report" {
		t.Fatalf("kind = %v, want status_report", parsed["kind"])
	}
	if gw.planCalls != 0 {
		t.Fatalf("GeneratePlan calls = %d, want 0", gw.planCalls)
	}
	if gw.analyzeCalls != 0 {
		t.Fatalf("AnalyzeScope calls = %d, want 0", gw.analyzeCalls)
	}
}

func TestPlanIntentFlowsThroughScopeAnalysis(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"messages":[{"role":"user","content":"Build me a REST API"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	if gw.intentCalls != 1 {
		t.Fatalf("ClassifyIntent calls = %d, want 1", gw.intentCalls)
	}
	if gw.analyzeCalls != 1 {
		t.Fatalf("AnalyzeScope calls = %d, want 1", gw.analyzeCalls)
	}
	if gw.planCalls != 1 {
		t.Fatalf("GeneratePlan calls = %d, want 1", gw.planCalls)
	}
}

func TestAmbiguousIntentReturnsClarification(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intent = &gateway.IntentAnalysis{Intent: "ambiguous", Reason: "greeting only"}
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"messages":[{"role":"user","content":"Hello"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	content := decodeBody(t, resp)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("clarification content is not JSON: %v", err)
	}
	if parsed["kind"] != "intent_clarification" {
		t.Fatalf("kind = %v, want intent_clarification", parsed["kind"])
	}
	if gw.planCalls != 0 {
		t.Fatalf("GeneratePlan calls = %d, want 0", gw.planCalls)
	}
}

func TestApprovedScopeBypassesIntentClassification(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"messages":[{"role":"user","content":"Build an API"}],"approved_scopes":["backend-api"]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)
	if gw.intentCalls != 0 {
		t.Fatalf("ClassifyIntent calls = %d, want 0", gw.intentCalls)
	}
	if gw.planCalls != 1 {
		t.Fatalf("GeneratePlan calls = %d, want 1", gw.planCalls)
	}
}
