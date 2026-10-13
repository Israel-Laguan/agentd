package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"agentd/internal/config"
)

func TestMapDraftAPIError_ProviderMessages(t *testing.T) {
	body := []byte(`{"status":"error","error":{"message":"internal error: no LLM providers configured"}}`)
	err := mapDraftAPIError(http.StatusInternalServerError, body)
	if !errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want %v", err, config.ErrNoLLMProviders)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want HTTP status in message", err)
	}
}

func TestDecodeDraft_EmptyChoices(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"choices":[]}`)),
	}
	_, err := decodeDraft(resp)
	if err == nil {
		t.Fatal("decodeDraft() error = nil, want missing choices error")
	}
	if !strings.Contains(err.Error(), "missing choices") {
		t.Fatalf("error = %v, want missing choices message", err)
	}
	if strings.Contains(err.Error(), "status OK") {
		t.Fatalf("error = %v, should not report misleading status OK", err)
	}
}

func TestMapDraftAPIError_Generic(t *testing.T) {
	err := mapDraftAPIError(http.StatusBadRequest, []byte(`{"error":"bad"}`))
	if errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want non-provider error", err)
	}
	if !strings.Contains(err.Error(), "Bad Request") {
		t.Fatalf("error = %v, want status text", err)
	}
}

func TestMapDraftAPIError_ShowsValidationMessage(t *testing.T) {
	body := []byte(`{"status":"error","error":{"code":"VALIDATION_FAILED","message":"planner returned an invalid plan: task 1: title is required"}}`)
	err := mapDraftAPIError(http.StatusUnprocessableEntity, body)
	if !strings.Contains(err.Error(), "task 1: title is required") {
		t.Fatalf("error = %v, want validation text", err)
	}
}

func TestDecodeDraft_NonPlanResponsesShowTheirMessage(t *testing.T) {
	for _, kind := range []string{"status_report", "intent_clarification", "feasibility_clarification", "scope_clarification"} {
		content := `{"kind":"` + kind + `","message":"No active projects. Send a plan request."}`
		payload, _ := json.Marshal(content)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":` + string(payload) + `}}]}`)),
		}
		_, err := decodeDraft(resp)
		if err == nil || !strings.Contains(err.Error(), "No active projects. Send a plan request.") {
			t.Fatalf("%s: error = %v, want response message", kind, err)
		}
	}
}

// B-044: a mid-plan provider outage returns HTTP 200 with the J09 [SYSTEM]
// timeout text (chat is 200 even when all providers are dead); ask must
// report the provider failure, not a JSON decode artifact.
func TestDecodeDraft_SystemTimeoutContentMapsToProviders(t *testing.T) {
	content := `[SYSTEM] Communication with AI core timed out. Please try your request again.`
	payload, _ := json.Marshal(content)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":` + string(payload) + `}}]}`)),
	}
	_, err := decodeDraft(resp)
	if err == nil {
		t.Fatal("decodeDraft() error = nil, want provider failure")
	}
	if !errors.Is(err, config.ErrNoLLMProviders) {
		t.Fatalf("error = %v, want %v", err, config.ErrNoLLMProviders)
	}
	if strings.Contains(err.Error(), "invalid character") {
		t.Fatalf("error = %v, must not surface a JSON parse artifact", err)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want the real timeout cause", err)
	}
}

// B-044: a non-JSON 200 body (proxy error page, plain-text upstream error)
// must name the body instead of `invalid character ...`.
func TestDecodeDraft_NonJSONEnvelopeBody(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{name: "html body", body: `<html><body>502 Bad Gateway</body></html>`, want: "Bad Gateway"},
		{name: "plain text", body: `Service Unavailable: LiteLLM overloaded`, want: "Service Unavailable"},
		{name: "empty body", body: ``, want: "empty response body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tt.body))}
			_, err := decodeDraft(resp)
			if err == nil {
				t.Fatal("decodeDraft() error = nil, want descriptive failure")
			}
			if strings.Contains(err.Error(), "invalid character") {
				t.Fatalf("error = %v, must not surface a JSON parse artifact", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

// B-044: non-200 provider failures must carry the real body text.
func TestMapDraftAPIError_NonJSONBodies(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "plain-text 5xx", status: http.StatusServiceUnavailable, body: `Service Unavailable: LiteLLM overloaded`, want: "Service Unavailable"},
		{name: "404 model-not-found text", status: http.StatusNotFound, body: `model not found: poolside/laguna-xs.2`, want: "model not found"},
		{name: "html body", status: http.StatusBadGateway, body: `<html><body>Bad Gateway</body></html>`, want: "Bad Gateway"},
		{name: "empty body", status: http.StatusInternalServerError, body: ``, want: "empty response body"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := mapDraftAPIError(tt.status, []byte(tt.body))
			if err == nil {
				t.Fatal("mapDraftAPIError() error = nil, want failure")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
