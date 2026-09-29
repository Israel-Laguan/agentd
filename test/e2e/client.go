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

// ResetBreaker calls POST /api/v1/system/breaker/reset, resetting the
// global circuit breaker (and all per-provider breakers) to CLOSED. The
// breaker is process-global, not per-project: while it's OPEN, new task
// dispatch is probe-limited (internal/queue/loop_dispatch.go's
// dispatchAvailable) for up to breaker.handoff_after (2m default) before
// the next task even gets attempted, so a journey that depends on a fresh
// breaker (like J07) should reset it first rather than assume a clean
// state left by a prior run.
func (c *APIClient) ResetBreaker(ctx context.Context) (*http.Response, error) {
	return c.Post(ctx, "/api/v1/system/breaker/reset", nil)
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

// MaterializePlanForUser is MaterializePlan with the X-Agentd-User header
// identifying who the project is for. The controller stamps that identity
// onto the project, which is what scopes the worker's memory recall — and
// therefore which saved preferences reach the execution prompt. Clients that
// omit it get a project with no user, and no preferences recalled.
func (c *APIClient) MaterializePlanForUser(ctx context.Context, plan DraftPlan, userID string) (*http.Response, error) {
	data, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/projects/materialize", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-Agentd-User", userID)
	}
	return c.client.Do(req)
}

// SavePreference calls POST /api/v1/preferences, recording a user preference
// as a USER_PREFERENCE memory. Returns 201 on success; there is no GET
// counterpart, so a journey observes the effect by checking a later task's
// prompt rather than by reading the stored row back.
func (c *APIClient) SavePreference(ctx context.Context, userID, text string) (*http.Response, error) {
	return c.PostJSON(ctx, "/api/v1/preferences", map[string]string{
		"user_id": userID,
		"text":    text,
	})
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
// Pass an empty state to list every task. Self-healing handoff tasks (HUMAN
// assignees created by a provider outage, see internal/queue/worker/
// worker_healing_handoff.go) are excluded unless includeHealing is true.
func (c *APIClient) ListTasks(ctx context.Context, projectID, state string, includeHealing bool) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/projects/%s/tasks", projectID)
	query := ""
	if state != "" {
		query += "state=" + state
	}
	if includeHealing {
		if query != "" {
			query += "&"
		}
		query += "include_healing=true"
	}
	if query != "" {
		path += "?" + query
	}
	return c.Get(ctx, path)
}

// DecodeTaskResponse reads and JSON-decodes a single-task envelope response,
// as returned by RetryTask, Patch, Assign, and Split.
func DecodeTaskResponse(resp *http.Response) (*Task, error) {
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		Data Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode task response: %w", err)
	}
	return &envelope.Data, nil
}

// TaskEvent is one entry of a task's event log
// (internal/api/controllers/tasks_events.go's taskEventResponse).
type TaskEvent struct {
	ID      string `json:"id"`
	TaskID  string `json:"task_id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// ListTaskEvents calls GET /api/v1/tasks/{taskID}/events and decodes the
// returned events (most recent 100 by default).
func (c *APIClient) ListTaskEvents(ctx context.Context, taskID string) ([]TaskEvent, error) {
	resp, err := c.Get(ctx, fmt.Sprintf("/api/v1/tasks/%s/events", taskID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list task events returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data []TaskEvent `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode task events: %w", err)
	}
	return envelope.Data, nil
}

// RetryTask calls POST /api/v1/tasks/{taskID}/retry, transitioning a
// FAILED/FAILED_REQUIRES_HUMAN/BLOCKED/IN_CONSIDERATION task back to READY
// (internal/services/task_service.go's Retry). This is the real recovery
// path for a provider-outage BLOCKED parent: fix the outage out-of-band,
// then retry the parent — its still-open ManualReview HUMAN child is not
// resolved through /human-resolution (that endpoint is reserved for
// manual-action handoffs only; see internal/kanban/human_handoff.go's
// validateHandoffChild).
func (c *APIClient) RetryTask(ctx context.Context, taskID string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/retry", taskID)
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
