package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agentd/internal/gateway/spec"
)

// LlamaCpp calls a llama.cpp /v1/chat/completions endpoint.
type LlamaCpp struct {
	cfg    spec.ProviderConfig
	client *http.Client
}

// NewLlamaCpp constructs a LlamaCpp backend.
func NewLlamaCpp(cfg spec.ProviderConfig, client *http.Client) *LlamaCpp {
	if client == nil {
		client = http.DefaultClient
	}
	return &LlamaCpp{cfg: cfg, client: client}
}

// Name implements Backend.
func (l *LlamaCpp) Name() spec.Provider {
	return spec.ProviderLlamaCpp
}

// MaxInputChars implements Backend.
func (l *LlamaCpp) MaxInputChars() int {
	return l.cfg.MaxInputChars
}

// Generate implements Backend.
func (l *LlamaCpp) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if l.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.cfg.Timeout)
		defer cancel()
	}
	model := l.cfg.Model
	if req.Model != "" {
		model = req.Model
	}
	body := openAIRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSONMode {
		body.ResponseFormat = map[string]string{"type": "json_object"}
	}
	data, _, err := postJSON(ctx, l.client, l.url(), body, "")
	if err != nil {
		return spec.AIResponse{}, err
	}
	var decoded openAIResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.AIResponse{}, fmt.Errorf("decode llamacpp response: %w", err)
	}
	return openAIResponseToLlamaCpp(decoded, model), nil
}

func (l *LlamaCpp) url() string {
	return strings.TrimRight(l.cfg.BaseURL, "/") + "/v1/chat/completions"
}

func openAIResponseToLlamaCpp(r openAIResponse, defaultModel string) spec.AIResponse {
	model := r.Model
	if model == "" {
		model = defaultModel
	}
	content := ""
	if len(r.Choices) > 0 && r.Choices[0].Message.Content != nil {
		content = *r.Choices[0].Message.Content
	}
	return spec.AIResponse{
		Content:      content,
		TokenUsage:   r.Usage.TotalTokens,
		ProviderUsed: string(spec.ProviderLlamaCpp),
		ModelUsed:    model,
	}
}