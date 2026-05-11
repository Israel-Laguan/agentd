package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func TestAgentHandler(t *testing.T) {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	h := controllers.AgentHandler{Service: svc}

	t.Run("Create", func(t *testing.T) {
		body := `{"id": "test-agent", "name": "Test Agent", "provider": "openai", "model": "gpt-4"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("Create code = %d body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("List", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		rec := httptest.NewRecorder()
		h.List(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("List code = %d", rec.Code)
		}
		var resp struct {
			Data []interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Data) == 0 {
			t.Fatal("expected at least one agent")
		}
	})

	t.Run("Get", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
		req.SetPathValue("id", "test-agent")
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Get code = %d", rec.Code)
		}
	})

	t.Run("Patch", func(t *testing.T) {
		body := `{"name": "Updated Agent"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/test-agent", strings.NewReader(body))
		req.SetPathValue("id", "test-agent")
		rec := httptest.NewRecorder()
		h.Patch(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Patch code = %d", rec.Code)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/test-agent", nil)
		req.SetPathValue("id", "test-agent")
		rec := httptest.NewRecorder()
		h.Delete(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Delete code = %d", rec.Code)
		}
	})
}
