//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
)

// ProjectManager tracks a journey's project name for isolation. There is no
// DELETE /api/v1/projects/{id} route in the real API, so cleanup is a no-op:
// each journey run leaves its project (and workspace) behind in the devenv
// stack's data volume.
type ProjectManager struct {
	client *APIClient
	name   string
}

// NewProjectManager creates a manager for a journey project.
// The name is typically a journey ID + random suffix for isolation, and
// becomes DraftPlan.ProjectName when materializing.
func NewProjectManager(client *APIClient, name string) *ProjectManager {
	return &ProjectManager{
		client: client,
		name:   name,
	}
}

// Name returns the project name.
func (p *ProjectManager) Name() string {
	return p.name
}

// Project represents a project object from the API (internal/models.Project).
type Project struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	OriginalInput string `json:"original_input"`
	WorkspacePath string `json:"workspace_path"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

// ListProjects retrieves all projects.
func ListProjects(ctx context.Context, client *APIClient) ([]Project, error) {
	resp, err := client.Projects(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{path: "/api/v1/projects", status: resp.StatusCode}
	}

	var envelope struct {
		Data []Project `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return envelope.Data, nil
}

// DraftPlan is the Frontdesk plan proposal (internal/models.DraftPlan). It
// is both what the chat endpoint returns for a plan-shaped intent and,
// unmodified, the request body for POST /api/v1/projects/materialize —
// there is no separate approval step or plan ID in the real API.
type DraftPlan struct {
	ProjectName string      `json:"project_name"`
	Description string      `json:"description,omitempty"`
	Tasks       []DraftTask `json:"tasks"`
	// SourcePath, when set, is copied into the workspace synchronously and
	// unlocks root tasks immediately. StartEmptyWorkspace does the same
	// without needing a source directory. Leaving both unset requires an
	// explicit WorkspaceReady call once the workspace has content.
	SourcePath          string `json:"source_path,omitempty"`
	StartEmptyWorkspace bool   `json:"start_empty_workspace,omitempty"`
}

// DraftTask is one task proposed within a DraftPlan.
type DraftTask struct {
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	DependsOn       []string `json:"depends_on,omitempty"`
}

// MaterializeResult is the response body of POST /api/v1/projects/materialize.
type MaterializeResult struct {
	Project Project `json:"project"`
	Tasks   []Task  `json:"tasks"`
}

// DecodeMaterializeResult reads and JSON-decodes a materialize response
// (status envelope: {"status":"success","data":{"project":...,"tasks":[...]}}).
func DecodeMaterializeResult(resp *http.Response) (*MaterializeResult, error) {
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		Data MaterializeResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

type statusError struct {
	path   string
	status int
}

func (e *statusError) Error() string {
	return e.path + " returned unexpected status " + http.StatusText(e.status)
}
