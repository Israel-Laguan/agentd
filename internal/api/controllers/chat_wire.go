package controllers

import (
	"bytes"
	"encoding/json"
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

// buildToolCalls inspects the JSON content returned by Frontdesk and
// constructs an OpenAI-compatible tool_calls slice when the content
// represents a plan or a status report. Tools are only emitted when the
// client opted in by sending req.Tools (preserves wire-shape stability for
// existing simple clients that just consume content). A "none" tool_choice
// suppresses tool calls, and a call is only emitted for a function name the
// client actually declared.
func buildToolCalls(content []byte, tools []json.RawMessage, toolChoice json.RawMessage) ([]chatToolCall, string) {
	if len(tools) == 0 || len(content) == 0 {
		return nil, finishReasonStop
	}
	args := string(bytes.TrimSpace(content))
	var probe struct {
		Kind  string            `json:"kind"`
		Tasks []json.RawMessage `json:"tasks"`
	}
	if err := json.Unmarshal(content, &probe); err != nil {
		return nil, finishReasonStop
	}
	var name string
	switch {
	case probe.Kind == "status_report":
		name = toolNameStatusReport
	case probe.Kind == "" && len(probe.Tasks) > 0:
		name = toolNameCreatePlan
	default:
		return nil, finishReasonStop
	}
	if !toolCallAllowed(name, tools, toolChoice) {
		return nil, finishReasonStop
	}
	return []chatToolCall{{
		ID:   "call_" + uuid.NewString(),
		Type: "function",
		Function: chatToolCallFunction{
			Name: name, Arguments: args,
		},
	}}, finishReasonToolCalls
}

// toolCallAllowed reports whether a tool call for name may be emitted given
// the client's declared tools and tool_choice.
func toolCallAllowed(name string, tools []json.RawMessage, toolChoice json.RawMessage) bool {
	if choice := string(bytes.TrimSpace(toolChoice)); len(choice) > 0 {
		var asString string
		if err := json.Unmarshal(toolChoice, &asString); err == nil {
			if asString == "none" {
				return false
			}
		} else {
			var choiceObj struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			}
			if err := json.Unmarshal(toolChoice, &choiceObj); err == nil && choiceObj.Function.Name != "" {
				if choiceObj.Function.Name != name {
					return false
				}
			}
		}
	}
	declared := declaredToolNames(tools)
	_, ok := declared[name]
	return ok
}

// declaredToolNames extracts the set of function names from an OpenAI-style
// tools array, accepting both {"function":{"name":...}} and {"name":...} shapes.
func declaredToolNames(tools []json.RawMessage) map[string]struct{} {
	names := make(map[string]struct{})
	for _, raw := range tools {
		var fn struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &fn); err != nil {
			continue
		}
		if fn.Function.Name != "" {
			names[fn.Function.Name] = struct{}{}
		} else if fn.Name != "" {
			names[fn.Name] = struct{}{}
		}
	}
	return names
}
