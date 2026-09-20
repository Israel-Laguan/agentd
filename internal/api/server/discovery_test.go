package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/server"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/testutil"
)

// newDiscoveryHandler builds the API mux with minimal deps for B-001
// discovery-endpoint tests.
func newDiscoveryHandler() http.Handler {
	store := testutil.NewFakeStore()
	return server.NewHandler(server.ServerDeps{
		Store:      store,
		Gateway:    stubGateway{},
		Bus:        bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
}

// TestDiscoveryRootServesLandingPage (B-001): GET / used to return the mux
// 404. It now identifies the daemon and links to /health, /docs, /api/v1.
func TestDiscoveryRootServesLandingPage(t *testing.T) {
	h := newDiscoveryHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("root is not JSON: %v", err)
	}
	for _, key := range []string{"service", "status", "health", "docs", "api"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("landing page missing %q: %v", key, body)
		}
	}
}

// TestDiscoveryHealthCheck (B-001): GET /health is a dependency-free
// liveness probe returning {"status":"ok"}.
func TestDiscoveryHealthCheck(t *testing.T) {
	h := newDiscoveryHandler()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("health is not JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health status = %v, want ok", body["status"])
	}
}

// TestDiscoveryDocsListsEndpoints (B-001): GET /docs serves a readable
// endpoint overview instead of the mux 404.
func TestDiscoveryDocsListsEndpoints(t *testing.T) {
	h := newDiscoveryHandler()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rec.Body.String(), "/api/v1") {
		t.Fatalf("docs page does not mention /api/v1: %s", rec.Body.String())
	}
}

// TestDiscoveryUnknownPathStill404: the root catch-all must not swallow the
// mux's 404 for genuinely unknown paths.
func TestDiscoveryUnknownPathStill404(t *testing.T) {
	h := newDiscoveryHandler()
	req := httptest.NewRequest(http.MethodGet, "/nope", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body.String())
	}
}
