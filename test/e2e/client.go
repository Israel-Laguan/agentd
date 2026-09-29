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

// SystemProjectID is the fixed UUID of agentd's `_system` project
// (internal/kanban/system_project.go's systemProjectID). The disk watchdog
// (internal/queue/disk_watchdog.go) and the outage handoff
// (internal/queue/outage_handoff.go) both attach their HUMAN tasks here, and
// `_system` is excluded from GET /api/v1/projects unless include_system=true
// — but it is addressable directly by ID, which is how a journey observes it.
const SystemProjectID = "00000000-0000-0000-0000-000000000001"

// Breaker states, mirroring internal/queue/safety.BreakerState.
const (
	BreakerClosed   = "CLOSED"
	BreakerOpen     = "OPEN"
	BreakerHalfOpen = "HALF_OPEN"
)

// BreakerSnapshot is the `breaker` field of the system/status response
// (internal/services.BreakerSnapshot). OpenFor is a bare time.Duration, so it
// serializes as an integer nanosecond count rather than "5m0s".
type BreakerSnapshot struct {
	State        string `json:"state"`
	FailureCount int    `json:"failure_count"`
	OpenFor      int64  `json:"open_for"`
	LastError    string `json:"last_error"`
}

// ProviderBreaker is one entry of the status response's provider_breakers map
// (internal/services.ProviderBreakerEntry), keyed by provider name. Only
// quota errors (ErrLLMQuotaExceeded) feed per-provider breakers; unreachable
// providers feed the single global breaker — see
// internal/queue/worker/worker_handoffs.go's HandleGatewayError.
type ProviderBreaker struct {
	State        string `json:"state"`
	FailureCount int    `json:"failure_count"`
}

// SystemStatusReport is the subset of GET /api/v1/system/status that journeys
// assert on. The endpoint also carries frontdesk status, memory, and rolling
// budget fields, which are not journey-relevant.
type SystemStatusReport struct {
	Breaker          *BreakerSnapshot           `json:"breaker"`
	ProviderBreakers map[string]ProviderBreaker `json:"provider_breakers"`
	RollingBudgetOn  bool                       `json:"rolling_budget_enabled"`
}

// DecodeSystemStatus reads and decodes a system/status response.
func DecodeSystemStatus(resp *http.Response) (*SystemStatusReport, error) {
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("system status returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data SystemStatusReport `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode system status: %w", err)
	}
	return &envelope.Data, nil
}

// SystemProjectTasks calls GET /api/v1/projects/_system/tasks and decodes the
// result. Used by J10 to observe the disk watchdog's HUMAN task. Pass
// includeHealing so tasks created by the self-healing ladder are listed (the
// disk task is HUMAN but its title is not the "Manual review required:"
// prefix, so it is listed either way).
func (c *APIClient) SystemProjectTasks(ctx context.Context, includeHealing bool) ([]Task, error) {
	path := SystemProjectID + "/tasks"
	if includeHealing {
		path += "?include_healing=true"
	}
	resp, err := c.Get(ctx, "/api/v1/projects/"+path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list _system tasks returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data []Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode _system tasks: %w", err)
	}
	return envelope.Data, nil
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

// humanResolutionRequest is the body for POST /api/v1/tasks/{id}/human-resolution
// (internal/api/controllers/tasks_human.go).
type humanResolutionRequest struct {
	Result string `json:"result"`
}

// ResolveHumanHandoff calls POST /api/v1/tasks/{taskID}/human-resolution.
// taskID is the HUMAN-assigned child task's ID (from ListTasks with
// includeHealing=true), not the parent it was created to unblock.
func (c *APIClient) ResolveHumanHandoff(ctx context.Context, taskID, result string) (*http.Response, error) {
	path := fmt.Sprintf("/api/v1/tasks/%s/human-resolution", taskID)
	return c.PostJSON(ctx, path, humanResolutionRequest{Result: result})
}

// HumanHandoffResolution is the response body of a resolved human handoff
// (internal/models.HumanHandoffResolution): the resolved HUMAN task and its
// now-completed parent.
type HumanHandoffResolution struct {
	Task   Task   `json:"task"`
	Parent Task   `json:"parent"`
	Result string `json:"result"`
}

// DecodeHumanHandoffResolution reads and JSON-decodes a human-resolution response.
func DecodeHumanHandoffResolution(resp *http.Response) (*HumanHandoffResolution, error) {
	defer func() { _ = resp.Body.Close() }()
	var envelope struct {
		Data HumanHandoffResolution `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode human handoff resolution: %w", err)
	}
	return &envelope.Data, nil
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
