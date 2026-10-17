package server_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/server"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
	"agentd/internal/testutil"
)

const crossSiteBody = `{"project_name":"pwned","tasks":[{"title":"x"}]}`

// A page on another origin can send a text/plain POST without a CORS preflight.
// The router must refuse it before any handler runs (B-064).
func TestRouterRefusesCrossSiteWrites(t *testing.T) {
	store := testutil.NewFakeStore()
	h := server.NewHandler(server.ServerDeps{
		Store: store, Gateway: stubGateway{}, Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/projects/materialize"},
		{http.MethodPatch, "/api/v1/tasks/t1"},
		{http.MethodPost, "/api/v1/tasks/t1/comments"},
		{http.MethodPost, "/v1/chat/completions"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(crossSiteBody))
		req.Host = "127.0.0.1:8765"
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Content-Type", "text/plain")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden && rec.Code != http.StatusUnsupportedMediaType {
			t.Errorf("%s %s: status = %d, want 403 or 415; body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
	if projects, _ := store.ListProjects(t.Context()); len(projects) != 0 {
		t.Fatalf("cross-site POST created %d project(s)", len(projects))
	}
}

func TestRouterRefusesRebindingHost(t *testing.T) {
	store := testutil.NewFakeStore()
	h := server.NewHandler(server.ServerDeps{
		Store: store, Gateway: stubGateway{}, Bus: bus.NewInProcess(),
		Summarizer: frontdesk.NewStatusSummarizer(store),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Host = "rebind.evil.example:8765"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
