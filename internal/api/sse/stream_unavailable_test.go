package sse

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// T-054: a missing bus answers with the shared error envelope, not plain text.
func TestStreamWithoutBusAnswersEnvelope(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	Handler{}.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var env struct {
		Status string `json:"status"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not the envelope: %v: %q", err, rec.Body.String())
	}
	if env.Status != "error" || env.Error.Code != "UNAVAILABLE" || env.Error.Message == "" {
		t.Fatalf("envelope = %+v", env)
	}
}
