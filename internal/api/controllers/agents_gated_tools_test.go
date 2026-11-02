package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
)

// agentGatedTools sends one agents request and returns gated_tools from the
// response, failing unless it answers wantCode.
func agentGatedTools(t *testing.T, h controllers.AgentHandler, method, body string, wantCode int) []string {
	t.Helper()
	req := httptest.NewRequest(method, "/api/v1/agents/gated", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "gated")
	rec := httptest.NewRecorder()
	switch method {
	case http.MethodPost:
		h.Create(rec, req)
	case http.MethodPatch:
		h.Patch(rec, req)
	default:
		h.Get(rec, req)
	}
	if rec.Code != wantCode {
		t.Fatalf("%s code = %d, want %d; body = %s", method, rec.Code, wantCode, rec.Body.String())
	}
	var resp struct {
		Data struct {
			GatedTools []string `json:"gated_tools"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data.GatedTools
}

// gated_tools is accepted on create and patch and returned on every read; a
// patch without it keeps the list, and an empty list clears it.
func TestAgentHandler_GatedToolsCreatePatchGet(t *testing.T) {
	h := agentTestHandler()
	created := agentGatedTools(t, h, http.MethodPost,
		`{"id":"gated","name":"Gated","provider":"openai","model":"gpt-4","gated_tools":[" bash "]}`, http.StatusCreated)
	if !slices.Equal(created, []string{"bash"}) {
		t.Fatalf("created gated_tools = %v, want [bash]", created)
	}
	if got := agentGatedTools(t, h, http.MethodGet, "", http.StatusOK); !slices.Equal(got, []string{"bash"}) {
		t.Fatalf("get gated_tools = %v, want [bash]", got)
	}
	if got := agentGatedTools(t, h, http.MethodPatch, `{"gated_tools":["deploy","write"]}`, http.StatusOK); !slices.Equal(got, []string{"deploy", "write"}) {
		t.Fatalf("patched gated_tools = %v, want [deploy write]", got)
	}
	if got := agentGatedTools(t, h, http.MethodPatch, `{"name":"Renamed"}`, http.StatusOK); !slices.Equal(got, []string{"deploy", "write"}) {
		t.Fatalf("gated_tools = %v after a patch without it, want it kept", got)
	}
	if got := agentGatedTools(t, h, http.MethodPatch, `{"gated_tools":[]}`, http.StatusOK); len(got) != 0 {
		t.Fatalf("gated_tools = %v after clearing, want none", got)
	}
}

// A blank name would gate nothing, so it is refused rather than stored.
func TestAgentHandler_GatedToolsRejectsBlankName(t *testing.T) {
	h := agentTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents",
		strings.NewReader(`{"id":"gated","name":"Gated","provider":"openai","model":"gpt-4","gated_tools":["bash"," "]}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	assertAgentValidationFailed(t, rec, "gated_tools")
}
