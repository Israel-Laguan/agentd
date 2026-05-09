package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"agentd/internal/gateway/spec"
)

// Ollama calls a local Ollama /api/chat endpoint.
type Ollama struct {
	cfg    spec.ProviderConfig
	client *http.Client
}

// NewOllama constructs an Ollama backend.
func NewOllama(cfg spec.ProviderConfig, client *http.Client) *Ollama {
	if client == nil {
		client = http.DefaultClient
	}
	return &Ollama{cfg: cfg, client: client}
}

// Name implements Backend.
func (o *Ollama) Name() spec.Provider {
	return spec.ProviderOllama
}

// MaxInputChars implements Backend.
func (o *Ollama) MaxInputChars() int {
	return o.cfg.MaxInputChars
}

// Generate implements Backend.
func (o *Ollama) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if o.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
		defer cancel()
	}
	model := o.cfg.Model
	if req.Model != "" {
		model = req.Model
	}
	body := ollamaRequest{
		Model:       model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
	}
	if req.JSONMode {
		body.Format = "json"
	}
	data, _, err := postJSON(ctx, o.client, o.url(), body, "")
	if err != nil {
		return spec.AIResponse{}, err
	}
	var decoded ollamaResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.AIResponse{}, fmt.Errorf("decode ollama response: %w", err)
	}
	return decoded.toAIResponse(model), nil
}

func (o *Ollama) url() string {
	return strings.TrimRight(o.cfg.BaseURL, "/") + "/api/chat"
}

type ollamaRequest struct {
	Model       string                `json:"model"`
	Messages    []spec.PromptMessage `json:"messages"`
	Temperature float64               `json:"temperature"`
	Format      string                `json:"format,omitempty"`
	Stream      bool                  `json:"stream"`
}

type ollamaResponse struct {
	Message         spec.PromptMessage `json:"message"`
	Model           string             `json:"model"`
	PromptEvalCount int                `json:"prompt_eval_count"`
	EvalCount       int                `json:"eval_count"`
}

func (r ollamaResponse) toAIResponse(defaultModel string) spec.AIResponse {
	model := r.Model
	if model == "" {
		model = defaultModel
	}
	return spec.AIResponse{
		Content:      r.Message.Content,
		TokenUsage:   r.PromptEvalCount + r.EvalCount,
		ProviderUsed: string(spec.ProviderOllama),
		ModelUsed:    model,
	}
}
