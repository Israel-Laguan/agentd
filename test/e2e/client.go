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

// chatMessage is the OpenAI-shaped message the real /v1/chat/completions
// endpoint expects (agentd/internal/gateway/spec.PromptMessage's wire
// shape, duplicated here to avoid importing internal packages from tests).
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionsRequest mirrors the OpenAI Chat Completions request body
// plus agentd's approved_scopes extension (internal/api/controllers/chat.go).
type chatCompletionsRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	ApprovedScopes []string      `json:"approved_scopes,omitempty"`
}

// ChatCompletions calls POST /v1/chat/completions with a single user
// message. approvedScopes disambiguates multi-scope planning (pass nil for
// a fresh conversation).
func (c *APIClient) ChatCompletions(ctx context.Context, message string, approvedScopes []string) (*http.Response, error) {
	body := chatCompletionsRequest{
		Model:          "agentd",
		Messages:       []chatMessage{{Role: "user", Content: message}},
		ApprovedScopes: approvedScopes,
	}
	return c.PostJSON(ctx, "/v1/chat/completions", body)
}

// ChatCompletion is the OpenAI-compatible response shape returned by
// /v1/chat/completions (internal/api/controllers/chat_wire.go).
type ChatCompletion struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// DecodeChatCompletion reads and JSON-decodes a chat completions response.
func DecodeChatCompletion(resp *http.Response) (*ChatCompletion, error) {
	defer func() { _ = resp.Body.Close() }()
	var out ChatCompletion
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode chat completion: %w", err)
	}
	return &out, nil
}

// MaterializePlan calls POST /api/v1/projects/materialize with the given
// draft plan body (internal/api/controllers/projects.go). There is no
// separate "approve" step or plan ID in the real API: the DraftPlan JSON
// returned by chat IS the materialize request body.
func (c *APIClient) MaterializePlan(ctx context.Context, plan DraftPlan) (*http.Response, error) {
	return c.PostJSON(ctx, "/api/v1/projects/materialize", plan)
}

// WorkspaceReady calls POST /api/v1/projects/{projectID}/workspace/ready.
// projectID is the project's UUID (assigned at materialize time), not its
// human-readable name.
func (c *APIClient) WorkspaceReady(ctx context.Context, projectID string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/workspace/ready", projectID)
	return c.Post(ctx, path, nil)
}

// ListTasks calls GET /api/v1/projects/{projectID}/tasks, optionally
// filtered by comma-separated state(s) (e.g. "COMPLETED" or "READY,RUNNING").
// Pass an empty state to list every task.
func (c *APIClient) ListTasks(ctx context.Context, projectID, state string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)
	if state != "" {
		path += "?state=" + state
	}
	return c.Get(ctx, path)
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
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
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
