package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"agentd/internal/api/correlation"
	"agentd/internal/gateway/spec"
)

// Ollama calls a local Ollama /api/chat endpoint.
type Ollama struct {
	common
}

// NewOllama constructs an Ollama backend.
func NewOllama(cfg spec.ProviderConfig, client *http.Client) *Ollama {
	if client == nil {
		client = http.DefaultClient
	}
	warnUnknownOptions("ollama", nil, cfg.Options)
	return &Ollama{common: common{
		name:             providerName(cfg, spec.ProviderOllama),
		cfg:              cfg,
		client:           client,
		chatToolsDefault: false,
	}}
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
	log := correlation.Logger(ctx)
	log.DebugContext(ctx, "ollama: sending request",
		"model", model, "message_count", len(req.Messages),
		"timeout", o.cfg.Timeout)
	start := time.Now()
	data, status, err := postJSON(ctx, o.client, o.url("/api/chat"), body, "")
	latency := time.Since(start)
	if err != nil {
		log.WarnContext(ctx, "ollama: request failed",
			"model", model, "status", status, "latency_ms", latency.Milliseconds(), "error", err)
		return spec.AIResponse{}, err
	}
	var decoded ollamaResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.AIResponse{}, fmt.Errorf("decode ollama response: %w", err)
	}
	resp := decoded.toAIResponse(model, string(o.Name()))
	log.DebugContext(ctx, "ollama: response received",
		"model", resp.ModelUsed, "tokens", resp.TokenUsage,
		"status", status, "latency_ms", latency.Milliseconds())
	return resp, nil
}

type ollamaRequest struct {
	Model       string               `json:"model"`
	Messages    []spec.PromptMessage `json:"messages"`
	Temperature float64              `json:"temperature"`
	Format      string               `json:"format,omitempty"`
	Stream      bool                 `json:"stream"`
}

type ollamaResponse struct {
	Message         spec.PromptMessage `json:"message"`
	Model           string             `json:"model"`
	PromptEvalCount int                `json:"prompt_eval_count"`
	EvalCount       int                `json:"eval_count"`
}

func (r ollamaResponse) toAIResponse(defaultModel string, providerUsed string) spec.AIResponse {
	model := r.Model
	if model == "" {
		model = defaultModel
	}
	return spec.AIResponse{
		Content:      r.Message.Content,
		TokenUsage:   r.PromptEvalCount + r.EvalCount,
		ProviderUsed: providerUsed,
		ModelUsed:    model,
	}
}
