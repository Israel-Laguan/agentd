package providers

import (
	"context"
	"encoding/json"
	"fmt"
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
	return &OpenAI{cfg: cfg, client: client}
}

// Name implements Backend.
func (o *OpenAI) Name() spec.Provider {
	return spec.ProviderOpenAI
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
	model := o.cfg.Model
	if req.Model != "" {
		model = req.Model
	}
	body := openAIRequest{
		Model:       model,
		Messages:    messagesToOpenAI(req.Messages),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	// OpenAI does not allow response_format: json_object when tools are present.
	// When tools are provided, we omit response_format to avoid conflicts.
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
	return decoded.toAIResponse(model), nil
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
	body := openAIEmbedRequest{
		Model: model,
		Input: req.Input,
	}
	data, _, err := postJSON(ctx, o.client, o.embeddingsURL(), body, o.cfg.APIKey)
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
	return spec.EmbedResponse{
		Vectors:      vectors,
		ProviderUsed: string(spec.ProviderOpenAI),
		ModelUsed:    modelUsed,
	}, nil
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
	return Capabilities{SupportsChatTools: true}
}

type openAIRequest struct {
	Model          string          `json:"model"`
	Messages       []openAIMessage `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	Tools          []openAITool    `json:"tools,omitempty"`
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
		om := openAIMessage{
			Role:       m.Role,
			Name:       m.Name,
			ToolCalls:  m.ToolCalls,
			ToolCallID: m.ToolCallID,
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 && m.Content == "" {
			om.Content = nil
		} else {
			content := m.Content
			om.Content = &content
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

func (r openAIResponse) toAIResponse(defaultModel string) spec.AIResponse {
	model := r.Model
	if model == "" {
		model = defaultModel
	}
	content := ""
	var toolCalls []spec.ToolCall
	if len(r.Choices) > 0 {
		msg := r.Choices[0].Message
		if msg.Content != nil {
			content = *msg.Content
		}
		if content == "" && msg.ReasoningContent != nil {
			content = *msg.ReasoningContent
		}
		if len(msg.ToolCalls) > 0 {
			toolCalls = make([]spec.ToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				toolCalls[i] = spec.ToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: spec.ToolCallFunction{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
	}
	return spec.AIResponse{
		Content:      content,
		TokenUsage:   r.Usage.TotalTokens,
		ProviderUsed: string(spec.ProviderOpenAI),
		ModelUsed:    model,
		ToolCalls:    toolCalls,
	}
}
