package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"agentd/internal/api/correlation"
	"agentd/internal/gateway/spec"
)

// LlamaCpp calls a llama.cpp /v1/chat/completions endpoint.
type LlamaCpp struct {
	common
	effectiveTools *bool
}

// NewLlamaCpp constructs a LlamaCpp backend.
func NewLlamaCpp(cfg spec.ProviderConfig, client *http.Client) *LlamaCpp {
	if client == nil {
		client = http.DefaultClient
	}
	warnUnknownOptions("llamacpp", []string{"probe_tools"}, cfg.Options)
	return &LlamaCpp{common: common{
		name:             providerName(cfg, spec.ProviderLlamaCpp),
		cfg:              cfg,
		client:           client,
		chatToolsDefault: false,
	}}
}

// Generate implements Backend.
func (l *LlamaCpp) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if l.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, l.cfg.Timeout)
		defer cancel()
	}
	model := req.Model
	if model == "" {
		model = l.cfg.Model
	}
	body := openAIRequest{
		Model:       model,
		Messages:    messagesToOpenAI(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	if req.JSONMode && len(req.Tools) == 0 {
		body.ResponseFormat = map[string]string{"type": "json_object"}
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]openAITool, len(req.Tools))
		for i, t := range req.Tools {
			body.Tools[i] = openAITool{Type: "function", Function: t}
		}
	}
	log := correlation.Logger(ctx)
	log.DebugContext(ctx, "llamacpp: sending request",
		"model", model, "endpoint", l.url("/v1/chat/completions"),
		"message_count", len(req.Messages), "tools_count", len(req.Tools),
		"timeout", l.cfg.Timeout)
	start := time.Now()
	data, status, err := postJSON(ctx, l.client, l.url("/v1/chat/completions"), body, "")
	latency := time.Since(start)
	if err != nil {
		log.WarnContext(ctx, "llamacpp: request failed",
			"model", model, "status", status, "latency_ms", latency.Milliseconds(), "error", err)
		return spec.AIResponse{}, err
	}
	var decoded openAIResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.AIResponse{}, fmt.Errorf("decode llamacpp response: %w", err)
	}
	resp := decoded.toAIResponse(model, string(l.Name()))
	log.DebugContext(ctx, "llamacpp: response received",
		"model", resp.ModelUsed, "tokens", resp.TokenUsage,
		"tool_calls", len(resp.ToolCalls),
		"status", status, "latency_ms", latency.Milliseconds())
	return resp, nil
}

// Capabilities implements Backend.
func (l *LlamaCpp) Capabilities() Capabilities {
	if l.effectiveTools != nil {
		return Capabilities{SupportsChatTools: *l.effectiveTools}
	}
	return l.common.Capabilities()
}

// ProbeTools sends a minimal tool-calling request when options.probe_tools is enabled
// and refines the effective SupportsChatTools capability. It never returns an error;
// failures are logged and treated as "not supported".
func (l *LlamaCpp) ProbeTools(ctx context.Context) bool {
	if !probeToolsEnabled(l.cfg) {
		return l.Capabilities().SupportsChatTools
	}
	if l.effectiveTools != nil {
		return *l.effectiveTools
	}

	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	body := openAIRequest{
		Model: l.cfg.Model,
		Messages: messagesToOpenAI([]spec.PromptMessage{
			{Role: "user", Content: "What is the weather?"},
		}),
		MaxTokens: 64,
		Tools: []openAITool{
			{Type: "function", Function: spec.ToolDefinition{
				Name:        "get_weather",
				Description: "Get weather for a location",
				Parameters: &spec.FunctionParameters{
					Type:       "object",
					Properties: map[string]any{"location": map[string]string{"type": "string"}},
					Required:   []string{"location"},
				},
			}},
		},
	}

	data, _, err := postJSON(probeCtx, l.client, l.url("/v1/chat/completions"), body, "")
	if err != nil {
		slog.Warn("llamacpp tool probe failed", "provider", l.Name(), "err", err)
		unsupported := false
		l.effectiveTools = &unsupported
		return false
	}

	var decoded openAIResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		slog.Warn("llamacpp tool probe decode failed", "provider", l.Name(), "err", err)
		unsupported := false
		l.effectiveTools = &unsupported
		return false
	}

	_, toolCalls := decoded.extractResponseContent()
	verdict := len(toolCalls) > 0
	l.effectiveTools = &verdict
	slog.Info("llamacpp tool probe", "provider", l.Name(), "supported", verdict)
	return verdict
}

func probeToolsEnabled(cfg spec.ProviderConfig) bool {
	return optionBool(cfg.Options, "probe_tools")
}
