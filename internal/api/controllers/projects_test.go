package controllers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/api/controllers"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/services"
	"agentd/internal/testutil"
)

func projectTestHandler() (controllers.ProjectHandler, *testutil.FakeKanbanStore) {
	store := testutil.NewFakeStore()
	return controllers.ProjectHandler{Store: store}, store
}

func projectServiceTestHandler(t *testing.T) (controllers.ProjectHandler, *sandbox.FSWorkspaceManager) {
	t.Helper()
	store := testutil.NewFakeStore()
	ws := &sandbox.FSWorkspaceManager{Root: t.TempDir()}
	svc := services.NewProjectService(store, ws)
	return controllers.ProjectHandler{Store: store, Service: svc}, ws
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

func projectHandlerWithMaterializeToken(t *testing.T, token string) (controllers.ProjectHandler, *sandbox.FSWorkspaceManager) {
	t.Helper()
	store := testutil.NewFakeStore()
	ws := &sandbox.FSWorkspaceManager{Root: t.TempDir()}
	svc := services.NewProjectService(store, ws)
	return controllers.ProjectHandler{
		Store:            store,
		Service:          svc,
		MaterializeToken: token,
	}, ws
}

func materializeProjectHTTP(t *testing.T, h controllers.ProjectHandler, body, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Agentd-Materialize-Token", token)
	}
	rec := httptest.NewRecorder()
	h.Materialize(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			Project struct {
				ID string `json:"ID"`
			} `json:"project"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data.Project.ID
}

func assertWorkspaceReadyStatus(t *testing.T, h controllers.ProjectHandler, projectID, token string, want int) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/workspace/ready", nil)
	req.SetPathValue("id", projectID)
	if token != "" {
		req.Header.Set("X-Agentd-Materialize-Token", token)
	}
	rec := httptest.NewRecorder()
	h.WorkspaceReady(rec, req)
	if rec.Code != want {
		t.Fatalf("code = %d body = %s", rec.Code, rec.Body.String())
	}
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

func TestProjectHandler_MaterializeWithSourcePath(t *testing.T) {
	h, ws := projectServiceTestHandler(t)
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := `{"project_name":"seeded-api","source_path":"` + srcDir + `","tasks":[{"title":"Build","description":"work"}]}`
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
				ID string `json:"ID"`
			} `json:"project"`
			Tasks []struct {
				State string `json:"state"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data.Tasks) != 1 {
		t.Fatalf("tasks len = %d", len(resp.Data.Tasks))
	}
	if resp.Data.Tasks[0].State != string(models.TaskStateReady) {
		t.Fatalf("task state = %q, want READY", resp.Data.Tasks[0].State)
	}

	helloPath := filepath.Join(ws.ProjectDir(resp.Data.Project.ID), "hello.txt")
	data, err := os.ReadFile(helloPath)
	if err != nil {
		t.Fatalf("read seeded hello.txt: %v", err)
	}
	if string(data) != "world" {
		t.Fatalf("hello.txt = %q, want %q", data, "world")
	}
}

func TestProjectHandler_WorkspaceReadyUnlocksTasks(t *testing.T) {
	h, ws := projectServiceTestHandler(t)

	body := `{"project_name":"ready-api","tasks":[{"title":"Work","description":"do it"}]}`
	matReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
	matReq.Header.Set("Content-Type", "application/json")
	matRec := httptest.NewRecorder()
	h.Materialize(matRec, matReq)
	if matRec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", matRec.Code, matRec.Body.String())
	}

	var matResp struct {
		Data struct {
			Project struct {
				ID string `json:"ID"`
			} `json:"project"`
			Tasks []struct {
				State string `json:"state"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(matRec.Body.Bytes(), &matResp); err != nil {
		t.Fatal(err)
	}
	projectID := matResp.Data.Project.ID
	if matResp.Data.Tasks[0].State != string(models.TaskStatePending) {
		t.Fatalf("task state = %q, want PENDING before workspace/ready", matResp.Data.Tasks[0].State)
	}

	seedPath := filepath.Join(ws.ProjectDir(projectID), "seed.txt")
	if err := os.WriteFile(seedPath, []byte("seeded"), 0o644); err != nil {
		t.Fatal(err)
	}

	readyReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/workspace/ready", nil)
	readyReq.SetPathValue("id", projectID)
	readyRec := httptest.NewRecorder()
	h.WorkspaceReady(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("WorkspaceReady code = %d body = %s", readyRec.Code, readyRec.Body.String())
	}

	var readyResp struct {
		Data struct {
			Tasks []struct {
				State string `json:"state"`
			} `json:"tasks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(readyRec.Body.Bytes(), &readyResp); err != nil {
		t.Fatal(err)
	}
	if len(readyResp.Data.Tasks) != 1 {
		t.Fatalf("unlocked tasks len = %d, want 1", len(readyResp.Data.Tasks))
	}
	if readyResp.Data.Tasks[0].State != string(models.TaskStateReady) {
		t.Fatalf("unlocked task state = %q, want READY", readyResp.Data.Tasks[0].State)
	}
}

func TestProjectHandler_WorkspaceReadyEmptyWorkspace(t *testing.T) {
	h, _ := projectServiceTestHandler(t)

	body := `{"project_name":"empty-ws","tasks":[{"title":"Work","description":"do it"}]}`
	matReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/materialize", strings.NewReader(body))
	matReq.Header.Set("Content-Type", "application/json")
	matRec := httptest.NewRecorder()
	h.Materialize(matRec, matReq)
	if matRec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", matRec.Code, matRec.Body.String())
	}
	var matResp struct {
		Data struct {
			Project struct {
				ID string `json:"ID"`
			} `json:"project"`
		} `json:"data"`
	}
	if err := json.Unmarshal(matRec.Body.Bytes(), &matResp); err != nil {
		t.Fatal(err)
	}
	projectID := matResp.Data.Project.ID

	readyReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/workspace/ready", nil)
	readyReq.SetPathValue("id", projectID)
	readyRec := httptest.NewRecorder()
	h.WorkspaceReady(readyRec, readyReq)
	if readyRec.Code != http.StatusConflict {
		t.Fatalf("WorkspaceReady code = %d body = %s, want 409", readyRec.Code, readyRec.Body.String())
	}
}

func TestProjectHandler_WorkspaceReadyToken(t *testing.T) {
	const token = "workspace-ready-secret"
	h, ws := projectHandlerWithMaterializeToken(t, token)
	matBody := `{"project_name":"token-ready","tasks":[{"title":"T","description":"d"}]}`
	projectID := materializeProjectHTTP(t, h, matBody, token)
	if err := os.WriteFile(filepath.Join(ws.ProjectDir(projectID), "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("forbidden without header", func(t *testing.T) {
		assertWorkspaceReadyStatus(t, h, projectID, "", http.StatusForbidden)
	})
	t.Run("forbidden with wrong token", func(t *testing.T) {
		assertWorkspaceReadyStatus(t, h, projectID, "wrong", http.StatusForbidden)
	})
	t.Run("ok with matching token", func(t *testing.T) {
		assertWorkspaceReadyStatus(t, h, projectID, token, http.StatusOK)
	})
}

func TestProjectHandler_WorkspaceReadyServiceNotConfigured(t *testing.T) {
	h, _ := projectTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/some-id/workspace/ready", nil)
	req.SetPathValue("id", "some-id")
	rec := httptest.NewRecorder()
	h.WorkspaceReady(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("WorkspaceReady code = %d body = %s, want 500", rec.Code, rec.Body.String())
	}
}
