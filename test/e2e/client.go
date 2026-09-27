//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// APIClient wraps HTTP calls to the agentd API with convenience methods.
type APIClient struct {
	baseURL string
	client  *http.Client
}

// NewAPIClient creates a new API client.
func NewAPIClient(baseURL string, client *http.Client) *APIClient {
	if client == nil {
		client = &http.Client{}
	}
	return &APIClient{
		baseURL: baseURL,
		client:  client,
	}
}

// SystemStatus calls GET /api/v1/system/status.
func (c *APIClient) SystemStatus(ctx context.Context) (*http.Response, error) {
	return c.Get(ctx, "/api/v1/system/status")
}

// Projects calls GET /api/v1/projects.
func (c *APIClient) Projects(ctx context.Context) (*http.Response, error) {
	return c.Get(ctx, "/api/v1/projects")
}

// Chat calls POST /api/v1/chat with the given request body.
func (c *APIClient) Chat(ctx context.Context, body interface{}) (*http.Response, error) {
	return c.PostJSON(ctx, "/api/v1/chat", body)
}

// MaterializePlan calls POST /api/v1/projects/{projectName}/plans/{planID}/materialize.
func (c *APIClient) MaterializePlan(ctx context.Context, projectName, planID string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/plans/%s/materialize", projectName, planID)
	return c.Post(ctx, path, nil)
}

// WorkspaceReady calls POST /api/v1/projects/{projectName}/workspace/ready.
func (c *APIClient) WorkspaceReady(ctx context.Context, projectName string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/workspace/ready", projectName)
	return c.Post(ctx, path, nil)
}

// Get makes a GET request.
func (c *APIClient) Get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

// Post makes a POST request with the given body (JSON or nil).
func (c *APIClient) Post(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

// PostJSON makes a POST request with JSON body.
func (c *APIClient) PostJSON(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path,
		bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}

// Patch makes a PATCH request.
func (c *APIClient) Patch(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+path,
		bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.client.Do(req)
}
