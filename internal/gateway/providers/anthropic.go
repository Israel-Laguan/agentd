package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agentd/internal/gateway/spec"
)

const anthropicAPIVersion = "2023-06-01"

// Anthropic calls the Anthropic Messages API.
type Anthropic struct {
	cfg    spec.ProviderConfig
	client *http.Client
}

// NewAnthropic constructs an Anthropic backend.
func NewAnthropic(cfg spec.ProviderConfig, client *http.Client) *Anthropic {
	if client == nil {
		client = http.DefaultClient
	}
	return &Anthropic{cfg: cfg, client: client}
}

// Name implements Backend.
func (a *Anthropic) Name() spec.Provider {
	return spec.ProviderAnthropic
}

// MaxInputChars implements Backend.
func (a *Anthropic) MaxInputChars() int {
	return a.cfg.MaxInputChars
}

// Generate implements Backend.
func (a *Anthropic) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if a.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, a.cfg.Timeout)
		defer cancel()
	}
	model := a.cfg.Model
	if req.Model != "" {
		model = req.Model
	}

	system, messages := splitSystemMessages(req.Messages)
	body := anthropicRequest{
		Model:       model,
		Messages:    messages,
		System:      system,
		MaxTokens:   anthropicMaxTokens(req.MaxTokens),
		Temperature: req.Temperature,
	}

	data, err := a.post(ctx, body)
	if err != nil {
		return spec.AIResponse{}, err
	}
	var decoded anthropicResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.AIResponse{}, fmt.Errorf("decode anthropic response: %w", err)
	}
	return decoded.toAIResponse(model), nil
}

func (a *Anthropic) post(ctx context.Context, body anthropicRequest) ([]byte, error) {
	url := strings.TrimRight(a.cfg.BaseURL, "/") + "/v1/messages"
	data, _, err := postJSONWithHeaders(ctx, a.client, url, body, map[string]string{
		"x-api-key":         a.cfg.APIKey,
		"anthropic-version": anthropicAPIVersion,
	})
	return data, err
}

func splitSystemMessages(messages []spec.PromptMessage) (string, []anthropicMessage) {
	var systemParts []string
	var out []anthropicMessage
	for _, m := range messages {
		if strings.EqualFold(m.Role, "system") {
			if s := strings.TrimSpace(m.Content); s != "" {
				systemParts = append(systemParts, s)
			}
			continue
		}
		out = append(out, anthropicMessage{Role: m.Role, Content: m.Content})
	}
	return strings.Join(systemParts, "\n"), out
}

func anthropicMaxTokens(requested int) int {
	if requested > 0 {
		return requested
	}
	return 1024
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Model string `json:"model"`
}

func (r anthropicResponse) toAIResponse(defaultModel string) spec.AIResponse {
	model := r.Model
	if model == "" {
		model = defaultModel
	}
	var content string
	for _, c := range r.Content {
		if c.Type == "text" {
			content = c.Text
			break
		}
	}
	return spec.AIResponse{
		Content:      content,
		TokenUsage:   r.Usage.InputTokens + r.Usage.OutputTokens,
		ProviderUsed: string(spec.ProviderAnthropic),
		ModelUsed:    model,
	}
}
