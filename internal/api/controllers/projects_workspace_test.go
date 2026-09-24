package controllers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"agentd/internal/api/controllers"
)

type workspaceProject struct {
	ID            string `json:"id"`
	WorkspacePath string `json:"workspace_path"`
}

func TestProjectHandler_WorkspacePathParity(t *testing.T) {
	h, _ := projectServiceTestHandler(t)
	rec := postMaterialize(t, h, `{"project_name":"workspace-parity","tasks":[{"title":"Build","description":"work"}]}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("Materialize code = %d body = %s", rec.Code, rec.Body.String())
	}
	var materialized struct {
		Data struct {
			Project workspaceProject `json:"project"`
		} `json:"data"`
	}
	decodeProjectJSON(t, rec.Body.Bytes(), &materialized)
	want := materialized.Data.Project
	if !filepath.IsAbs(want.WorkspacePath) {
		t.Fatalf("materialize workspace_path = %q, want absolute path", want.WorkspacePath)
	}
	assertProjectGetWorkspace(t, h, want)
	assertProjectListWorkspace(t, h, want)
}

func assertProjectGetWorkspace(t *testing.T, h controllers.ProjectHandler, want workspaceProject) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+want.ID, nil)
	req.SetPathValue("id", want.ID)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Get code = %d body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Data workspaceProject `json:"data"`
	}
	decodeProjectJSON(t, rec.Body.Bytes(), &got)
	if got.Data.WorkspacePath != want.WorkspacePath {
		t.Fatalf("get workspace_path = %q, materialize = %q", got.Data.WorkspacePath, want.WorkspacePath)
	}
}

func assertProjectListWorkspace(t *testing.T, h controllers.ProjectHandler, want workspaceProject) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("List code = %d body = %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Data []workspaceProject `json:"data"`
	}
	decodeProjectJSON(t, rec.Body.Bytes(), &got)
	for _, project := range got.Data {
		if project.ID == want.ID {
			if project.WorkspacePath != want.WorkspacePath {
				t.Fatalf("list workspace_path = %q, materialize = %q", project.WorkspacePath, want.WorkspacePath)
			}
			return
		}
	}
	t.Fatalf("materialized project %q missing from list", want.ID)
}

func decodeProjectJSON(t *testing.T, body []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatal(err)
	}
}
