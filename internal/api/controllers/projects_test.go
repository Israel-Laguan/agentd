package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func projectTestHandler() (controllers.ProjectHandler, *testutil.FakeKanbanStore) {
	store := testutil.NewFakeStore()
	return controllers.ProjectHandler{Store: store}, store
}

func seedProject(t *testing.T, store *testutil.FakeKanbanStore) string {
	t.Helper()
	proj, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "seeded-project",
		Tasks:       []models.DraftTask{{Title: "Bootstrap", Description: "init"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	return proj.ID
}

func TestProjectHandler_List(t *testing.T) {
	h, store := projectTestHandler()
	seedProject(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("List code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
		Meta struct {
			Total int `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) == 0 {
		t.Fatal("expected at least one project")
	}
	if resp.Meta.Total != len(resp.Data) {
		t.Fatalf("meta.total = %d, data len = %d", resp.Meta.Total, len(resp.Data))
	}
}

func TestProjectHandler_Get(t *testing.T) {
	h, store := projectTestHandler()
	projectID := seedProject(t, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID, nil)
	req.SetPathValue("id", projectID)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Get code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.ID != projectID {
		t.Fatalf("id = %q, want %q", resp.Data.ID, projectID)
	}
}

func TestProjectHandler_GetNotFound(t *testing.T) {
	h, _ := projectTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/missing", nil)
	req.SetPathValue("id", "missing")
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("Get code = %d", rec.Code)
	}
}

func TestProjectHandler_Materialize(t *testing.T) {
	h, store := projectTestHandler()

	body := `{"project_name":"api-project","tasks":[{"title":"First","description":"work"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Materialize(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Project struct {
				Name string `json:"name"`
			} `json:"project"`
			Tasks []struct {
				Title string `json:"title"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Data.Project.Name != "api-project" {
		t.Fatalf("project name = %q", resp.Data.Project.Name)
	}
	if len(resp.Data.Tasks) != 1 {
		t.Fatalf("tasks len = %d", len(resp.Data.Tasks))
	}
	projects, err := store.ListProjects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) == 0 {
		t.Fatal("expected materialized project in store")
	}
}

func TestProjectHandler_MaterializeToken(t *testing.T) {
	store := testutil.NewFakeStore()
	h := controllers.ProjectHandler{
		Store:            store,
		MaterializeToken: "expected-secret-token",
	}
	body := `{"project_name":"token-project","tasks":[{"title":"T","description":"d"}]}`

	t.Run("forbidden without header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.Materialize(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
		}
		projects, err := store.ListProjects(context.Background())
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(projects) != 0 {
			t.Fatalf("expected no persisted projects on forbidden request, got %d", len(projects))
		}
	})

	t.Run("forbidden with wrong token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agentd-Materialize-Token", "wrong")
		rec := httptest.NewRecorder()
		h.Materialize(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("code = %d", rec.Code)
		}
		projects, err := store.ListProjects(context.Background())
		if err != nil {
			t.Fatalf("ListProjects: %v", err)
		}
		if len(projects) != 0 {
			t.Fatalf("expected no persisted projects on forbidden request, got %d", len(projects))
		}
	})

	t.Run("created with matching token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Agentd-Materialize-Token", "expected-secret-token")
		rec := httptest.NewRecorder()
		h.Materialize(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
		}
	})
}

func TestProjectHandler_MaterializeInvalidJSON(t *testing.T) {
	h, _ := projectTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader("{"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Materialize(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}
