package controllers

import (
	"time"

	"github.com/google/uuid"
)

const (
	systemTimeoutMessage = "[SYSTEM] Communication with AI core timed out. Please try your request again."

	chatObjectCompletion = "chat.completion"
	chatObjectChunk      = "chat.completion.chunk"

	finishReasonStop      = "stop"
	finishReasonToolCalls = "tool_calls"

	toolNameCreatePlan   = "create_plan"
	toolNameStatusReport = "status_report"
)

// chatCompletion is the minimal OpenAI-compatible response. Round-tripping
// through openai.ChatCompletion in tests ensures wire compatibility.
type chatCompletion struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type chatMessage struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

type chatToolCall struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function chatToolCallFunction `json:"function"`
}

type chatToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// chatChunk is the SSE frame for stream:true responses.
type chatChunk struct {
	ID      string            `json:"id"`
	Object  string            `json:"object"`
	Created int64             `json:"created"`
	Model   string            `json:"model"`
	Choices []chatChunkChoice `json:"choices"`
}

type chatChunkChoice struct {
	Index        int            `json:"index"`
	Delta        chatChunkDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type chatChunkDelta struct {
	Role      string         `json:"role,omitempty"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []chatToolCall `json:"tool_calls,omitempty"`
}

func completion(model, content string, toolCalls []chatToolCall, finishReason string) chatCompletion {
	if model == "" {
		model = "agentd"
	}
	contentPtr := &content
	return chatCompletion{
		ID: "chatcmpl-" + uuid.NewString(), Object: chatObjectCompletion,
		Created: time.Now().Unix(), Model: model,
		Choices: []chatChoice{{
			Index:        0,
			FinishReason: finishReason,
			Message: chatMessage{
				Role:      "assistant",
				Content:   contentPtr,
				ToolCalls: toolCalls,
			},
		}},
	}
}

func ptr[T any](v T) *T { return &v }
