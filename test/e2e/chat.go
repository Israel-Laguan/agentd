//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// This file holds the OpenAI-compatible chat surface of
// POST /v1/chat/completions: the request and response wire shapes and the
// convenience methods that build them. The generic HTTP verbs live in
// client.go.

// chatMessage is the OpenAI-shaped message the real /v1/chat/completions
// endpoint expects (agentd/internal/gateway/spec.PromptMessage's wire
// shape, duplicated here to avoid importing internal packages from tests).
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletionsRequest mirrors the OpenAI Chat Completions request body
// plus agentd's approved_scopes extension (internal/api/controllers/chat.go).
type chatCompletionsRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	ApprovedScopes []string        `json:"approved_scopes,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	Tools          []ToolDecl      `json:"tools,omitempty"`
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"`
}

// ChatCompletions calls POST /v1/chat/completions with a single user
// message. approvedScopes disambiguates multi-scope planning (pass nil for
// a fresh conversation).
func (c *APIClient) ChatCompletions(ctx context.Context, message string, approvedScopes []string) (*http.Response, error) {
	body := chatCompletionsRequest{
		Model:          "agentd",
		Messages:       []chatMessage{{Role: "user", Content: message}},
		ApprovedScopes: approvedScopes,
	}
	return c.PostJSON(ctx, "/v1/chat/completions", body)
}

// ToolDecl is one entry of an OpenAI-shaped request `tools` array. Only the
// function name is load-bearing: declaring a tool is what opts the client in
// to tool_calls in the response (internal/api/controllers/chat_wire.go's
// buildToolCalls emits a call only for a name the client actually declared).
type ToolDecl struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	} `json:"function"`
}

// ToolCall is one entry of a response's `tool_calls` array.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatCompletion is the OpenAI-compatible response shape returned by
// /v1/chat/completions (internal/api/controllers/chat_wire.go). The struct
// mirrors every field the handler emits, including the ones the daemon
// populates inconsistently: `created` is always set, while `usage` is
// declared omitempty and never populated on the non-streaming path.
type ChatCompletion struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Usage   *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Choices []struct {
		Index        int    `json:"index"`
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role      string     `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
}

// DecodeChatCompletion reads and JSON-decodes a chat completions response.
func DecodeChatCompletion(resp *http.Response) (*ChatCompletion, error) {
	defer func() { _ = resp.Body.Close() }()
	var out ChatCompletion
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode chat completion: %w", err)
	}
	return &out, nil
}

// ChatCompletionsWithTools is ChatCompletions with the OpenAI `tools` /
// `tool_choice` opt-in fields. Declaring tools is what makes the handler emit
// a `tool_calls` array (finish_reason "tool_calls") instead of plain content;
// toolChoice is sent verbatim when non-nil, e.g. the string "none" to
// suppress calls for a client that declared tools but wants plain content.
func (c *APIClient) ChatCompletionsWithTools(ctx context.Context, message string, tools []ToolDecl, toolChoice any) (*http.Response, error) {
	body := chatCompletionsRequest{
		Model:    "agentd",
		Messages: []chatMessage{{Role: "user", Content: message}},
	}
	if toolChoice != nil {
		raw, err := json.Marshal(toolChoice)
		if err != nil {
			return nil, err
		}
		body.ToolChoice = raw
	}
	if len(tools) > 0 {
		body.Tools = tools
	}
	return c.PostJSON(ctx, "/v1/chat/completions", body)
}

// ChatCompletionsStream is ChatCompletions with stream:true. The handler then
// answers with an SSE stream of `chat.completion.chunk` frames terminated by
// `data: [DONE]`, so the caller reads the body as a stream rather than
// decoding JSON (see ReadStreamFrames).
func (c *APIClient) ChatCompletionsStream(ctx context.Context, message string, tools []ToolDecl) (*http.Response, error) {
	body := map[string]any{
		"model":    "agentd",
		"messages": []chatMessage{{Role: "user", Content: message}},
		"stream":   true,
	}
	if len(tools) > 0 {
		body["tools"] = tools
	}
	return c.PostJSON(ctx, "/v1/chat/completions", body)
}
