//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ProjectManager manages project lifecycle for journeys (creation, cleanup, isolation).
type ProjectManager struct {
	client  *APIClient
	name    string
	created bool
}

// NewProjectManager creates a manager for a journey project.
// The name is typically a journey ID + random suffix for isolation.
func NewProjectManager(client *APIClient, name string) *ProjectManager {
	return &ProjectManager{
		client: client,
		name:   name,
	}
}

// Create creates a new project for the journey.
func (p *ProjectManager) Create(ctx context.Context) error {
	// Projects are typically created via the chat API or directly.
	// For now, we rely on MaterializePlan to auto-create the project.
	p.created = true
	return nil
}

// Name returns the project name.
func (p *ProjectManager) Name() string {
	return p.name
}

// Cleanup removes the project (best-effort; errors are logged but don't fail).
func (p *ProjectManager) Cleanup(ctx context.Context) error {
	if !p.created {
		return nil
	}

	// DELETE /api/v1/projects/{projectName}
	path := fmt.Sprintf("/api/v1/projects/%s", p.name)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, p.client.baseURL+path, nil)
	if err != nil {
		return err
	}

	resp, err := p.client.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	// Accept 200, 204, or 404 (already deleted).
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("cleanup returned %d", resp.StatusCode)
	}
	return nil
}

// Project represents a project object from the API.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// ListProjects retrieves all projects.
func ListProjects(ctx context.Context, client *APIClient) ([]Project, error) {
	resp, err := client.Projects(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list projects returned %d", resp.StatusCode)
	}

	var projects []Project
	if err := json.NewDecoder(resp.Body).Decode(&projects); err != nil {
		return nil, err
	}
	return projects, nil
}

// Plan represents a plan object from the API.
type Plan struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Title     string `json:"title"`
	State     string `json:"state"` // e.g., "DRAFT", "APPROVED", "MATERIALIZED"
	CreatedAt string `json:"created_at"`
}

// MaterializePlanRequest is the body for materializing a plan.
type MaterializePlanRequest struct {
	Scenario string `json:"scenario,omitempty"`
}
