package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api"
	"agentd/internal/bus"
	"agentd/internal/frontdesk"
)

func TestMaterializeForbiddenWithoutTokenWhenRequired(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(),
		Summarizer:       frontdesk.NewStatusSummarizer(store),
		MaterializeToken: "expected-secret-token",
	})
	body := `{"project_name":"p","tasks":[{"title":"t","description":"d"}]}`
	resp := request(handler, http.MethodPost, "/api/v1/projects/materialize", body)
	assertStatus(t, resp, http.StatusForbidden)
}

func TestMaterializeSucceedsWithMatchingToken(t *testing.T) {
	store := newAPITestStore()
	handler := api.NewHandler(api.ServerDeps{
		Store: store, Gateway: newAPIGateway(), Bus: bus.NewInProcess(),
		Summarizer:       frontdesk.NewStatusSummarizer(store),
		MaterializeToken: "expected-secret-token",
	})
	body := `{"project_name":"p","tasks":[{"title":"t","description":"d"}]}`
	resp := requestWithHeader(handler, http.MethodPost, "/api/v1/projects/materialize", body, "X-Agentd-Materialize-Token", "expected-secret-token")
	assertStatus(t, resp, http.StatusCreated)
}

func requestWithHeader(handler http.Handler, method, path, body, key, val string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set(key, val)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
