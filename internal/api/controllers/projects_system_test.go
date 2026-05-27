package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/testutil"
)

func TestProjectHandler_ListExcludesSystem(t *testing.T) {
	store := testutil.NewFakeStore()
	h := controllers.ProjectHandler{Store: store}

	seedProject(t, store)
	if _, err := store.EnsureSystemProject(context.Background()); err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	for _, p := range resp.Data {
		if p.Name == "_system" {
			t.Fatal("_system project should be excluded by default")
		}
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 project, got %d", len(resp.Data))
	}
}

func TestProjectHandler_ListIncludesSystemWhenRequested(t *testing.T) {
	store := testutil.NewFakeStore()
	h := controllers.ProjectHandler{Store: store}

	seedProject(t, store)
	if _, err := store.EnsureSystemProject(context.Background()); err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?include_system=true", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var foundSystem bool
	for _, p := range resp.Data {
		if p.Name == "_system" {
			foundSystem = true
		}
	}
	if !foundSystem {
		t.Fatal("_system project should be included with include_system=true")
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(resp.Data))
	}
}
