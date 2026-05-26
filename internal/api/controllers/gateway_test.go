package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/gateway/spec"
)

func TestGatewayListEmpty(t *testing.T) {
	h := controllers.GatewayHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/providers", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	var env struct {
		Data []any `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data == nil || len(env.Data) != 0 {
		t.Fatalf("expected empty array, got %v", env.Data)
	}
}

func TestGatewayListProviders(t *testing.T) {
	h := controllers.GatewayHandler{
		Configs: []spec.ProviderConfig{
			{Name: "gemini", Adapter: "openai", Model: "gemini-2.5-flash"},
			{Name: "local", Adapter: "ollama", Model: "llama3:8b"},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/providers", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	var env struct {
		Data []struct {
			Name    string   `json:"name"`
			Adapter string   `json:"adapter"`
			Models  []string `json:"models"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(env.Data))
	}
	first := env.Data[0]
	if first.Name != "gemini" || first.Adapter != "openai" {
		t.Errorf("first = %+v", first)
	}
	if len(first.Models) != 1 || first.Models[0] != "gemini-2.5-flash" {
		t.Errorf("first.Models = %v", first.Models)
	}
}

func TestGatewayListNoModel(t *testing.T) {
	h := controllers.GatewayHandler{
		Configs: []spec.ProviderConfig{
			{Name: "horde", Adapter: "horde", Model: ""},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/gateway/providers", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var env struct {
		Data []struct {
			Models []string `json:"models"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data[0].Models) != 0 {
		t.Errorf("expected empty models, got %v", env.Data[0].Models)
	}
}
