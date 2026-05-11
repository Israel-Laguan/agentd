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
	newHandler := func() controllers.AgentHandler {
		store := testutil.NewFakeStore()
		svc := services.NewAgentService(store, nil)
		return controllers.AgentHandler{Service: svc}
	}

	seedAgent := func(t *testing.T, h controllers.AgentHandler) {
		t.Helper()
		body := `{"id":"test-agent","name":"Test Agent","provider":"openai","model":"gpt-4"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed create code = %d body = %s", rec.Code, rec.Body.String())
		}
	}

	t.Run("Create", func(t *testing.T) {
		h := newHandler()
		body := `{"id": "test-agent", "name": "Test Agent", "provider": "openai", "model": "gpt-4"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("Create code = %d body = %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("List", func(t *testing.T) {
		h := newHandler()
		seedAgent(t, h)
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
		h := newHandler()
		seedAgent(t, h)
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
		req.SetPathValue("id", "test-agent")
		rec := httptest.NewRecorder()
		h.Get(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Get code = %d", rec.Code)
		}
	})

	t.Run("Patch", func(t *testing.T) {
		h := newHandler()
		seedAgent(t, h)
		body := `{"name": "Updated Agent"}`
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/test-agent", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", "test-agent")
		rec := httptest.NewRecorder()
		h.Patch(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Patch code = %d", rec.Code)
		}
		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
		getReq.SetPathValue("id", "test-agent")
		getRec := httptest.NewRecorder()
		h.Get(getRec, getReq)
		if getRec.Code != http.StatusOK {
			t.Fatalf("Get after patch code = %d body = %s", getRec.Code, getRec.Body.String())
		}
		var getResp struct {
			Data struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
			t.Fatal(err)
		}
		if getResp.Data.Name != "Updated Agent" {
			t.Fatalf("expected updated name %q, got %q", "Updated Agent", getResp.Data.Name)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		h := newHandler()
		seedAgent(t, h)
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/agents/test-agent", nil)
		req.SetPathValue("id", "test-agent")
		rec := httptest.NewRecorder()
		h.Delete(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("Delete code = %d", rec.Code)
		}

		getReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
		getReq.SetPathValue("id", "test-agent")
		getRec := httptest.NewRecorder()
		h.Get(getRec, getReq)
		if getRec.Code != http.StatusNotFound {
			t.Fatalf("expected not found after delete, got %d body = %s", getRec.Code, getRec.Body.String())
		}
	})
}