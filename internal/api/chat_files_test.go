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

func TestChatCompletionStashesOversizedUserMessageAndReadsForPlanning(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intent = &gateway.IntentAnalysis{Intent: "plan_request", Reason: "large build request"}
	input := strings.Repeat("a", 80) + strings.Repeat("b", 80)
	stash := &frontdesk.FileStash{Dir: t.TempDir(), StashThreshold: 20}
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store),
		FileStash: stash, Truncator: gateway.StrategyTruncator{Strategy: gateway.HeadTailStrategy{HeadRatio: 1}}, Budget: 40,
	})

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": input}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := request(handler, http.MethodPost, "/v1/chat/completions", string(body))
	assertStatus(t, resp, http.StatusOK)

	if strings.Contains(gw.lastClassifyIntent, input) {
		t.Fatalf("ClassifyIntent received oversized inline content")
	}
	if !strings.Contains(gw.lastClassifyIntent, "[agentd file reference]") {
		t.Fatalf("ClassifyIntent missing file reference: %q", gw.lastClassifyIntent)
	}
	if !strings.Contains(gw.lastPlanIntent, "[agentd file content]") {
		t.Fatalf("GeneratePlan missing file content block: %q", gw.lastPlanIntent)
	}
	if strings.Contains(gw.lastPlanIntent, strings.Repeat("b", 20)) {
		t.Fatalf("GeneratePlan received untruncated tail content: %q", gw.lastPlanIntent)
	}
}

func TestChatCompletionAcceptsFileContentReferences(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.intent = &gateway.IntentAnalysis{Intent: "plan_request", Reason: "file-backed request"}
	stash := &frontdesk.FileStash{Dir: t.TempDir(), StashThreshold: 1000}
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store),
		FileStash: stash, Truncator: gateway.StrategyTruncator{Strategy: gateway.HeadTailStrategy{HeadRatio: 0.5}}, Budget: 80,
	})

	body, err := json.Marshal(map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "Plan from the attached spec"}},
		"files": []map[string]string{{
			"name":    "spec.txt",
			"content": strings.Repeat("first ", 20) + strings.Repeat("last ", 20),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := request(handler, http.MethodPost, "/v1/chat/completions", string(body))
	assertStatus(t, resp, http.StatusOK)

	if !strings.Contains(gw.lastClassifyIntent, "name: spec.txt") {
		t.Fatalf("ClassifyIntent missing file name reference: %q", gw.lastClassifyIntent)
	}
	if !strings.Contains(gw.lastPlanIntent, "content:") {
		t.Fatalf("GeneratePlan missing read file content: %q", gw.lastPlanIntent)
	}
}
