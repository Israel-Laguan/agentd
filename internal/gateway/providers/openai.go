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
		Messages:    req.Messages,
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

// Capabilities implements Backend.
func (o *OpenAI) Capabilities() Capabilities {
	return Capabilities{SupportsChatTools: true}
}

type openAIRequest struct {
	Model          string                  `json:"model"`
	Messages       []spec.PromptMessage    `json:"messages"`
	Temperature    float64                 `json:"temperature"`
	MaxTokens      int                     `json:"max_tokens,omitempty"`
	ResponseFormat map[string]string       `json:"response_format,omitempty"`
	Tools          []openAITool            `json:"tools,omitempty"`
}

type openAITool struct {
	Type     string               `json:"type"`
	Function spec.ToolDefinition  `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Role             string        `json:"role"`
			Content          *string       `json:"content"`
			ReasoningContent *string       `json:"reasoning_content"`
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
					ID:       tc.ID,
					Type:     tc.Type,
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
