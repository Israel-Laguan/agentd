//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// mockLLMBaseURL is the host-published address of the devenv mock LLM. It is
// published (not just exposed) so a journey can read back the requests the
// worker sent without shelling into the container — see
// devenv/compose.yaml's mockllm service.
const mockLLMBaseURL = "http://localhost:8000"

// MockLLMClient reads the request capture log the devenv mock LLM maintains
// (devenv/mockllm/server.py's record_request, served at GET /requests).
//
// This exists because nothing in the agentd API exposes prompt contents: a
// journey that needs to assert what the worker actually sent to the model has
// no other channel. The capture is also what makes J11 a real test rather
// than a self-fulfilling one.
type MockLLMClient struct {
	baseURL string
	client  *http.Client
}

// NewMockLLMClient creates a client for the devenv mock LLM.
func NewMockLLMClient(baseURL string) *MockLLMClient {
	return &MockLLMClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// capturedRequest is one recorded chat-completions request. The shape mirrors
// the OpenAI wire format agentd's openai adapter posts
// (internal/gateway/providers/openai.go), with agentd's metadata extension
// (task_id / agent_id / role) either inline or re-added by litellm's
// correlation hook.
type capturedRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Metadata       map[string]string `json:"metadata"`
	AgentdMetadata map[string]string `json:"agentd_metadata"`
}

// taskID returns the correlation task id, which the mock's request_metadata
// helper treats as authoritative (litellm strips plain `metadata`, then
// devenv/litellm/agentd_correlation.py re-sends it as `agentd_metadata`).
func (c capturedRequest) taskID() string {
	if id := strings.TrimSpace(c.AgentdMetadata["task_id"]); id != "" {
		return id
	}
	return strings.TrimSpace(c.Metadata["task_id"])
}

// prompt renders the request's messages back into a single searchable blob.
func (c capturedRequest) prompt() string {
	var b strings.Builder
	for _, m := range c.Messages {
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// Requests returns every captured request, oldest first.
func (m *MockLLMClient) Requests(ctx context.Context) ([]capturedRequest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.baseURL+"/requests", nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mock /requests returned %d", resp.StatusCode)
	}
	var envelope struct {
		Data []capturedRequest `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode mock /requests: %w", err)
	}
	return envelope.Data, nil
}

// WorkerPrompts returns the rendered prompts of every captured request
// attributed to taskID, in arrival order.
func (m *MockLLMClient) WorkerPrompts(ctx context.Context, taskID string) ([]string, error) {
	requests, err := m.Requests(ctx)
	if err != nil {
		return nil, err
	}
	var prompts []string
	for _, r := range requests {
		if r.taskID() != taskID {
			continue
		}
		prompts = append(prompts, r.prompt())
	}
	return prompts, nil
}

// SetOutage takes the mock model named model down (every chat completion for it
// answers 503) or brings it back, via the mock's POST /outage. Only requests for
// that model are affected, so a journey that owns a model name can end a provider
// outage mid-test without touching any other journey.
func (m *MockLLMClient) SetOutage(ctx context.Context, model string, down bool) error {
	payload, err := json.Marshal(map[string]any{"model": model, "down": down})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/outage", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mock /outage returned %d", resp.StatusCode)
	}
	return nil
}

// postControl sends a control request to one of the mock's toggle endpoints.
func (m *MockLLMClient) postControl(ctx context.Context, path string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mock %s returned %d", path, resp.StatusCode)
	}
	return nil
}

// SetQuota makes the mock answer 429 for the named model (POST /quota), or stops
// doing so.
//
// This is deliberately not SetOutage. An outage is an unreachable provider and
// feeds the single global breaker, whereas only ErrLLMQuotaExceeded feeds the
// per-provider breakers (worker_handoffs.go). A journey exercising a provider
// breaker's probe slot needs quota errors. Keyed by model, so it affects only the
// profile that owns that model name.
func (m *MockLLMClient) SetQuota(ctx context.Context, model string, on bool) error {
	if model == "" {
		return errors.New("mock quota: model name is required")
	}
	return m.postControl(ctx, "/quota", map[string]any{"model": model, "on": on})
}

// SetSlowOnce arms a one-shot delay for the next request to the named model
// (POST /slow_once). That request sleeps and then proceeds normally; the delay is
// consumed, so only one call is slowed.
//
// A journey needs this to hold a breaker's probe in flight long enough to observe
// what the siblings do while it runs. Without it the probe resolves in
// milliseconds and no sibling ever sees AdmissionProbeInFlight.
func (m *MockLLMClient) SetSlowOnce(ctx context.Context, model string, seconds float64) error {
	if model == "" {
		return errors.New("mock slow_once: model name is required")
	}
	if seconds < 0 {
		return errors.New("mock slow_once: seconds must be >= 0")
	}
	return m.postControl(ctx, "/slow_once", map[string]any{"model": model, "seconds": seconds})
}
