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

func TestOutOfScopeIntentReturnsFeasibilityClarification(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intent = &gateway.IntentAnalysis{Intent: "out_of_scope", Reason: "non-software fantasy"}
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	body := `{"messages":[{"role":"user","content":"Make me a million dollars overnight"}]}`
	resp := request(handler, http.MethodPost, "/v1/chat/completions", body)
	assertStatus(t, resp, http.StatusOK)

	content := decodeBody(t, resp)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("content is not JSON: %v", err)
	}
	if parsed["kind"] != "feasibility_clarification" {
		t.Fatalf("kind = %v, want feasibility_clarification", parsed["kind"])
	}
	if gw.planCalls != 0 {
		t.Fatalf("GeneratePlan calls = %d, want 0", gw.planCalls)
	}
	if gw.analyzeCalls != 0 {
		t.Fatalf("AnalyzeScope calls = %d, want 0", gw.analyzeCalls)
	}
}
