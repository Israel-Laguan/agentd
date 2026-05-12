package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// AI Horde is an opt-in provider with queue semantics: requests are submitted
// asynchronously and polled for completion. It is selected by adding "horde" to
// gateway.order; the default provider order remains [openai, ollama]. Production
// deployments should use a real API key instead of the anonymous default.
const (
	defaultHordePollInterval = 4 * time.Second
	defaultHordeTimeout      = 5 * time.Minute
	defaultHordeAPIKey       = "0000000000"
	defaultHordeMaxLength    = 512
	hordeClientAgent         = "agentd:0:agentd"
)

// Horde calls the AI Horde async text API.
type Horde struct {
	cfg          spec.ProviderConfig
	client       *http.Client
	pollInterval time.Duration
	timeout      time.Duration
}

// NewHorde constructs a Horde backend.
func NewHorde(cfg spec.ProviderConfig, client *http.Client) *Horde {
	if client == nil {
		client = http.DefaultClient
	}
	pollInterval := cfg.PollInterval
	if pollInterval <= 0 {
		pollInterval = defaultHordePollInterval
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultHordeTimeout
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		cfg.APIKey = defaultHordeAPIKey
	}
	return &Horde{cfg: cfg, client: client, pollInterval: pollInterval, timeout: timeout}
}

// Name implements Backend.
func (h *Horde) Name() spec.Provider {
	return spec.ProviderHorde
}

// MaxInputChars implements Backend.
func (h *Horde) MaxInputChars() int {
	return h.cfg.MaxInputChars
}

// Capabilities implements Backend.
func (h *Horde) Capabilities() Capabilities {
	return Capabilities{SupportsChatTools: false}
}

// Generate implements Backend.
func (h *Horde) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()

	model := h.cfg.Model
	if req.Model != "" {
		model = req.Model
	}
	body := hordeSubmitRequest{
		Prompt: messagesToHordePrompt(req.Messages),
		Params: hordeParams{
			N:           1,
			MaxLength:   hordeMaxLength(req.MaxTokens),
			Temperature: req.Temperature,
		},
	}
	if strings.TrimSpace(model) != "" {
		body.Models = []string{model}
	}

	requestID, err := h.submit(ctx, body)
	if err != nil {
		return spec.AIResponse{}, err
	}
	return h.poll(ctx, requestID, model)
}

func (h *Horde) submit(ctx context.Context, body hordeSubmitRequest) (string, error) {
	data, err := h.doJSON(ctx, http.MethodPost, h.url("/v2/generate/text/async"), body)
	if err != nil {
		return "", err
	}
	var decoded hordeAsyncResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", fmt.Errorf("decode horde async response: %w", err)
	}
	if decoded.ID == "" {
		return "", fmt.Errorf("%w: horde did not return a request id", models.ErrLLMUnreachable)
	}
	return decoded.ID, nil
}

func (h *Horde) poll(ctx context.Context, requestID, defaultModel string) (spec.AIResponse, error) {
	ticker := time.NewTicker(h.pollInterval)
	defer ticker.Stop()

	for {
		status, err := h.status(ctx, requestID)
		if err != nil {
			return spec.AIResponse{}, err
		}
		if status.Faulted {
			return spec.AIResponse{}, fmt.Errorf("%w: horde request faulted", models.ErrLLMUnreachable)
		}
		if !status.IsPossible {
			return spec.AIResponse{}, fmt.Errorf("%w: horde request is not possible with current workers", models.ErrLLMUnreachable)
		}
		if status.Done {
			if len(status.Generations) == 0 {
				return spec.AIResponse{}, fmt.Errorf("%w: horde request completed without generations", models.ErrLLMUnreachable)
			}
			generation := status.Generations[0]
			model := generation.Model
			if model == "" {
				model = defaultModel
			}
			return spec.AIResponse{
				Content:      generation.Text,
				ProviderUsed: string(spec.ProviderHorde),
				ModelUsed:    model,
			}, nil
		}

		select {
		case <-ctx.Done():
			return spec.AIResponse{}, fmt.Errorf("%w: horde generation timed out: %v", models.ErrLLMUnreachable, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (h *Horde) status(ctx context.Context, requestID string) (hordeStatusResponse, error) {
	data, err := h.doJSON(ctx, http.MethodGet, h.url("/v2/generate/text/status/"+requestID), nil)
	if err != nil {
		return hordeStatusResponse{}, err
	}
	var decoded hordeStatusResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return hordeStatusResponse{}, fmt.Errorf("decode horde status response: %w", err)
	}
	return decoded, nil
}

func (h *Horde) doJSON(ctx context.Context, method, url string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal horde request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("create horde request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Client-Agent", hordeClientAgent)
	if strings.TrimSpace(h.cfg.APIKey) != "" {
		req.Header.Set("apikey", h.cfg.APIKey)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, fmt.Errorf("%w: horde request canceled: %v", models.ErrLLMUnreachable, ctx.Err())
		}
		return nil, fmt.Errorf("%w: send horde request: %v", models.ErrLLMUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("read horde response: %w", readErr)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, models.ErrLLMQuotaExceeded
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, fmt.Errorf("%w: horde status %d", models.ErrLLMUnreachable, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("horde rejected request: status %d", resp.StatusCode)
	}
	return data, nil
}

func (h *Horde) url(path string) string {
	return strings.TrimRight(h.cfg.BaseURL, "/") + path
}

func hordeMaxLength(maxTokens int) int {
	if maxTokens > 0 {
		return maxTokens
	}
	return defaultHordeMaxLength
}

func messagesToHordePrompt(messages []spec.PromptMessage) string {
	var parts []string
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(msg.Role)) {
		case "system":
			parts = append(parts, "System:\n"+content)
		case "assistant":
			parts = append(parts, "Assistant:\n"+content)
		default:
			parts = append(parts, "User:\n"+content)
		}
	}
	parts = append(parts, "Assistant:")
	return strings.Join(parts, "\n\n")
}

type hordeSubmitRequest struct {
	Prompt string      `json:"prompt"`
	Params hordeParams `json:"params"`
	Models []string    `json:"models,omitempty"`
}

type hordeParams struct {
	N           int     `json:"n"`
	MaxLength   int     `json:"max_length"`
	Temperature float64 `json:"temperature"`
}

type hordeAsyncResponse struct {
	ID string `json:"id"`
}

type hordeStatusResponse struct {
	Done        bool              `json:"done"`
	Faulted     bool              `json:"faulted"`
	WaitTime    int               `json:"wait_time"`
	IsPossible  bool              `json:"is_possible"`
	Generations []hordeGeneration `json:"generations"`
}

type hordeGeneration struct {
	Text  string `json:"text"`
	Model string `json:"model"`
}
