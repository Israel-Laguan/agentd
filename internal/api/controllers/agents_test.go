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

func agentTestHandler() controllers.AgentHandler {
	store := testutil.NewFakeStore()
	svc := services.NewAgentService(store, nil)
	return controllers.AgentHandler{Service: svc}
}

func seedAgent(t *testing.T, h controllers.AgentHandler) {
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

func TestAgentHandler_Create(t *testing.T) {
	h := agentTestHandler()
	body := `{"id": "test-agent", "name": "Test Agent", "provider": "openai", "model": "gpt-4"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create code = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestAgentHandler_List(t *testing.T) {
	h := agentTestHandler()
	seedAgent(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List code = %d", rec.Code)
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected at least one agent")
	}
	for _, a := range resp.Data {
		if a.ID == "test-agent" {
			return
		}
	}
	t.Fatalf("expected seeded agent %q in list response", "test-agent")
}

func TestAgentHandler_Get(t *testing.T) {
	h := agentTestHandler()
	seedAgent(t, h)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
	req.SetPathValue("id", "test-agent")
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Get code = %d", rec.Code)
	}
}

func TestAgentHandler_Patch(t *testing.T) {
	h := agentTestHandler()
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
}

func TestAgentHandler_CreateWithAgenticMode(t *testing.T) {
	h := agentTestHandler()
	body := `{"id":"agentic-agent","name":"Agentic Agent","provider":"openai","model":"gpt-4","agentic_mode":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Create code = %d body = %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Data struct {
			AgenticMode bool `json:"agentic_mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	if !createResp.Data.AgenticMode {
		t.Fatal("expected agentic_mode true in create response")
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agentic-agent", nil)
	getReq.SetPathValue("id", "agentic-agent")
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("Get code = %d body = %s", getRec.Code, getRec.Body.String())
	}
	var getResp struct {
		Data struct {
			AgenticMode bool `json:"agentic_mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatal(err)
	}
	if !getResp.Data.AgenticMode {
		t.Fatal("expected agentic_mode true after create")
	}
}

func TestAgentHandler_PatchAgenticMode(t *testing.T) {
	h := agentTestHandler()
	seedAgent(t, h)

	patchBody := `{"agentic_mode": true}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/test-agent", strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "test-agent")
	rec := httptest.NewRecorder()
	h.Patch(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Patch enable code = %d body = %s", rec.Code, rec.Body.String())
	}
	var patchResp struct {
		Data struct {
			AgenticMode bool `json:"agentic_mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &patchResp); err != nil {
		t.Fatal(err)
	}
	if !patchResp.Data.AgenticMode {
		t.Fatal("expected agentic_mode true after patch")
	}

	disableBody := `{"agentic_mode": false}`
	disableReq := httptest.NewRequest(http.MethodPatch, "/api/v1/agents/test-agent", strings.NewReader(disableBody))
	disableReq.Header.Set("Content-Type", "application/json")
	disableReq.SetPathValue("id", "test-agent")
	disableRec := httptest.NewRecorder()
	h.Patch(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("Patch disable code = %d body = %s", disableRec.Code, disableRec.Body.String())
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/agents/test-agent", nil)
	getReq.SetPathValue("id", "test-agent")
	getRec := httptest.NewRecorder()
	h.Get(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("Get after disable patch code = %d body = %s", getRec.Code, getRec.Body.String())
	}
	var getResp struct {
		Data struct {
			AgenticMode bool `json:"agentic_mode"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatal(err)
	}
	if getResp.Data.AgenticMode {
		t.Fatal("expected agentic_mode false after disable patch")
	}
}

func TestAgentHandler_Delete(t *testing.T) {
	h := agentTestHandler()
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
}
