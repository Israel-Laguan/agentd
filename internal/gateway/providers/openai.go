package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"agentd/internal/gateway/spec"
)

// OpenAI calls the OpenAI-compatible chat completions API.
type OpenAI struct {
	cfg    spec.ProviderConfig
	client *http.Client
}

// NewOpenAI constructs an OpenAI backend using cfg (BaseURL should include /v1 prefix when needed).
func NewOpenAI(cfg spec.ProviderConfig, client *http.Client) *OpenAI {
	if client == nil {
		client = http.DefaultClient
	}
	warnUnknownOptions("openai", []string{"send_task_metadata"}, cfg.Options)
	return &OpenAI{cfg: cfg, client: client}
}

// Name implements Backend. Returns the configured provider type so that
// OpenAI-compatible providers (e.g. Gemini) report their correct identity.
func (o *OpenAI) Name() spec.Provider {
	return providerName(o.cfg, spec.ProviderOpenAI)
}

// MaxInputChars implements Backend.
func (o *OpenAI) MaxInputChars() int {
	return o.cfg.MaxInputChars
}

// Generate implements Backend.
func (o *OpenAI) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if o.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
		defer cancel()
	}
	model := req.Model
	if model == "" {
		model = o.cfg.Model
	}
	body := openAIRequest{
		Model: model, Messages: messagesToOpenAI(req.Messages),
		Temperature: req.Temperature, MaxTokens: req.MaxTokens,
	}
	if optionBool(o.cfg.Options, "send_task_metadata") && (req.TaskID != "" || req.AgentID != "") {
		body.Metadata = map[string]string{}
		if req.TaskID != "" {
			body.Metadata["task_id"] = req.TaskID
		}
		if req.AgentID != "" {
			body.Metadata["agent_id"] = req.AgentID
		}
		if req.Role != "" {
			body.Metadata["role"] = string(req.Role)
		}
	}
	// OpenAI does not allow response_format: json_object when tools are present.
	if req.JSONMode && len(req.Tools) == 0 {
		body.ResponseFormat = map[string]string{"type": "json_object"}
	}
	if len(req.Tools) > 0 {
		body.Tools = make([]openAITool, len(req.Tools))
		for i, t := range req.Tools {
			body.Tools[i] = openAITool{Type: "function", Function: t}
		}
	}
	data, _, err := postJSON(ctx, o.client, o.url(), body, o.cfg.APIKey)
	if err != nil {
		return spec.AIResponse{}, err
	}
	var decoded openAIResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.AIResponse{}, fmt.Errorf("decode openai response: %w", err)
	}
	return decoded.toAIResponse(model, string(o.Name())), nil
}

func (o *OpenAI) url() string {
	return strings.TrimRight(o.cfg.BaseURL, "/") + "/chat/completions"
}

func (o *OpenAI) embeddingsURL() string {
	return strings.TrimRight(o.cfg.BaseURL, "/") + "/embeddings"
}

var _ EmbedBackend = (*OpenAI)(nil)

// Embed implements EmbedBackend using the OpenAI embeddings API.
func (o *OpenAI) Embed(ctx context.Context, req spec.EmbedRequest) (spec.EmbedResponse, error) {
	if len(req.Input) == 0 {
		return spec.EmbedResponse{Vectors: nil}, nil
	}
	if o.cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.cfg.Timeout)
		defer cancel()
	}
	model := req.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	data, _, err := postJSON(ctx, o.client, o.embeddingsURL(), openAIEmbedRequest{Model: model, Input: req.Input}, o.cfg.APIKey)
	if err != nil {
		return spec.EmbedResponse{}, err
	}
	var decoded openAIEmbedResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		return spec.EmbedResponse{}, fmt.Errorf("decode openai embeddings: %w", err)
	}
	vectors := make([][]float32, len(req.Input))
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(vectors) {
			continue
		}
		vectors[item.Index] = item.Embedding
	}
	modelUsed := decoded.Model
	if modelUsed == "" {
		modelUsed = model
	}
	return spec.EmbedResponse{Vectors: vectors, ProviderUsed: string(o.Name()), ModelUsed: modelUsed}, nil
}

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedResponse struct {
	Data  []openAIEmbedData `json:"data"`
	Model string            `json:"model"`
}

type openAIEmbedData struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// Capabilities implements Backend.
func (o *OpenAI) Capabilities() Capabilities {
	return capabilitiesFromConfig(o.cfg, true)
}

type openAIRequest struct {
	Model          string            `json:"model"`
	Messages       []openAIMessage   `json:"messages"`
	Temperature    float64           `json:"temperature"`
	MaxTokens      int               `json:"max_tokens,omitempty"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	Tools          []openAITool      `json:"tools,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type openAIMessage struct {
	Role       string          `json:"role"`
	Content    *string         `json:"content,omitempty"`
	Name       string          `json:"name,omitempty"`
	ToolCalls  []spec.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
}

func messagesToOpenAI(msgs []spec.PromptMessage) []openAIMessage {
	out := make([]openAIMessage, len(msgs))
	for i, m := range msgs {
		om := openAIMessage{Role: m.Role, Name: m.Name, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.Content == "" {
			om.Content = nil
		} else {
			om.Content = &m.Content
		}
		out[i] = om
	}
	return out
}

type openAITool struct {
	Type     string              `json:"type"`
	Function spec.ToolDefinition `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role             string           `json:"role"`
			Content          *string          `json:"content"`
			ReasoningContent *string          `json:"reasoning_content"`
			ToolCalls        []openAIToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
		// PromptTokensDetails carries OpenAI prompt-token breakdowns
		// (https://platform.openai.com/docs/api-reference/chat/object).
		// CachedTokens is the prompt-cache read count. Absent on most providers.
		PromptTokensDetails *struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details,omitempty"`
		// PromptCacheHitTokens / PromptCacheMissTokens are DeepSeek's
		// OpenAI-compatible cache fields. Hit = cache reads; miss = tokens
		// written to cache. Absent on OpenAI proper.
		PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
		PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
	} `json:"usage"`
	Model string `json:"model"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (r openAIResponse) toAIResponse(defaultModel, providerUsed string) spec.AIResponse {
	model := r.resolveModel(defaultModel)
	content, toolCalls := r.extractResponseContent()
	r.logZeroUsage(providerUsed, model)
	cached, cacheWrite := r.extractCacheMetrics(providerUsed, model)
	var usageDetails *spec.UsageDetails
	if cached != 0 || cacheWrite != 0 {
		usageDetails = &spec.UsageDetails{CachedTokens: cached, CacheWriteTokens: cacheWrite}
	}
	return spec.AIResponse{
		Content: content, TokenUsage: r.Usage.TotalTokens,
		ProviderUsed: providerUsed, ModelUsed: model,
		ToolCalls: toolCalls, UsageDetails: usageDetails,
	}
}

func (r openAIResponse) resolveModel(defaultModel string) string {
	if r.Model != "" {
		return r.Model
	}
	return defaultModel
}

func (r openAIResponse) extractResponseContent() (string, []spec.ToolCall) {
	if len(r.Choices) == 0 {
		return "", nil
	}
	msg := r.Choices[0].Message
	content := ""
	if msg.Content != nil {
		content = *msg.Content
	} else if msg.ReasoningContent != nil {
		content = *msg.ReasoningContent
	}
	var toolCalls []spec.ToolCall
	if len(msg.ToolCalls) > 0 {
		toolCalls = r.convertToolCalls(msg.ToolCalls)
	}
	return content, toolCalls
}

func (r openAIResponse) convertToolCalls(tcList []openAIToolCall) []spec.ToolCall {
	toolCalls := make([]spec.ToolCall, len(tcList))
	for i, tc := range tcList {
		toolCalls[i] = spec.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: spec.ToolCallFunction{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}
	}
	return toolCalls
}

func (r openAIResponse) logZeroUsage(providerUsed, model string) {
	if r.Usage.TotalTokens == 0 {
		slog.Debug("openai provider returned zero total_tokens; usage data may be absent",
			"provider", providerUsed, "model", model)
	}
}

func (r openAIResponse) extractCacheMetrics(providerUsed, model string) (int, int) {
	// Surface prompt-cache: OpenAI uses prompt_tokens_details.cached_tokens;
	// DeepSeek uses prompt_cache_hit_tokens (reads) and prompt_cache_miss_tokens (writes).
	cached := 0
	if r.Usage.PromptTokensDetails != nil {
		cached = r.Usage.PromptTokensDetails.CachedTokens
	}
	if r.Usage.PromptCacheHitTokens > cached {
		cached = r.Usage.PromptCacheHitTokens
	}
	cacheWrite := r.Usage.PromptCacheMissTokens
	if cached > 0 || cacheWrite > 0 {
		slog.Debug("openai provider parsed prompt-cache usage details",
			"provider", providerUsed, "model", model, "total_tokens", r.Usage.TotalTokens,
			"cached_tokens", cached, "cache_write_tokens", cacheWrite)
	}
	return cached, cacheWrite
}
