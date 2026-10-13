package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/models"
)

// B-030: a planner that exhausts its JSON repair budget must surface the
// validation text as a 422, not an opaque 500.
func TestInvalidPlannerJSONReturns422WithValidationText(t *testing.T) {
	store := newAPITestStore()
	gw := newAPIGateway()
	gw.planErr = fmt.Errorf("%w: task 1: title is required; raw response: {}", models.ErrInvalidJSONResponse)
	handler := api.NewHandler(api.ServerDeps{Store: store, Gateway: gw, Bus: bus.NewInProcess(), Summarizer: frontdesk.NewStatusSummarizer(store)})

	resp := request(handler, http.MethodPost, "/v1/chat/completions", `{"messages":[{"role":"user","content":"Build me a REST API"}]}`)
	assertStatus(t, resp, http.StatusUnprocessableEntity)
	errObj, _ := decodeBody(t, resp)["error"].(map[string]any)
	msg, _ := errObj["message"].(string)
	if !strings.Contains(msg, "title is required") {
		t.Fatalf("error message = %q, want validation text", msg)
	}
}
