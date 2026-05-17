package spec

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFunctionParametersMarshalJSON_NoArgumentSchema(t *testing.T) {
	tests := []struct {
		name   string
		params FunctionParameters
	}{
		{
			name:   "zero value",
			params: FunctionParameters{},
		},
		{
			name: "empty initialized containers",
			params: FunctionParameters{
				Properties: map[string]any{},
				Required:   []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}

			var gotObj map[string]any
			if err := json.Unmarshal(data, &gotObj); err != nil {
				t.Fatalf("Unmarshal(got) error = %v", err)
			}

			wantObj := map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []any{},
				"additionalProperties": false,
			}
			if !reflect.DeepEqual(gotObj, wantObj) {
				t.Fatalf("Marshal() = %v, want %v", gotObj, wantObj)
			}
		})
	}
}

func TestToolDefinitionMarshalJSON_IncludesParametersForNil(t *testing.T) {
	data, err := json.Marshal(ToolDefinition{
		Name:        "ping",
		Description: "Ping the service",
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	params, ok := got["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters not present or not an object in JSON: %s", data)
	}
	if params["type"] != "object" {
		t.Errorf("parameters.type = %v, want object", params["type"])
	}
	if params["additionalProperties"] != false {
		t.Errorf("parameters.additionalProperties = %v, want false", params["additionalProperties"])
	}
}

func toolConversationMessages() []PromptMessage {
	return []PromptMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What's the weather?"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_abc", Type: "function", Function: ToolCallFunction{
			Name: "get_weather", Arguments: `{"location":"Boston"}`,
		}}}},
		{Role: "tool", ToolCallID: "call_abc", Content: `{"temp":72,"conditions":"sunny"}`},
		{Role: "assistant", Content: "It's sunny and 72°F in Boston."},
	}
}

func TestPromptMessage_MarshalToolConversation(t *testing.T) {
	data, err := json.Marshal(toolConversationMessages())
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	assertMarshaledToolConversation(t, parsed)
}

func assertMarshaledToolConversation(t *testing.T, parsed []map[string]any) {
	t.Helper()
	if len(parsed) != 5 {
		t.Fatalf("len(messages) = %d, want 5", len(parsed))
	}
	assertMarshaledSystemMessage(t, parsed[0])
	assertMarshaledUserMessage(t, parsed[1])
	assertMarshaledAssistantToolCallsMessage(t, parsed[2])
	assertMarshaledToolResultMessage(t, parsed[3])
	assertMarshaledFinalAssistantMessage(t, parsed[4])
}

func assertMarshaledSystemMessage(t *testing.T, msg map[string]any) {
	t.Helper()
	if msg["role"] != "system" || msg["content"] != "You are a helpful assistant." {
		t.Errorf("system message = %v", msg)
	}
}

func assertMarshaledUserMessage(t *testing.T, msg map[string]any) {
	t.Helper()
	if msg["role"] != "user" || msg["content"] != "What's the weather?" {
		t.Errorf("user message = %v", msg)
	}
}

func assertMarshaledAssistantToolCallsMessage(t *testing.T, msg map[string]any) {
	t.Helper()
	if msg["role"] != "assistant" {
		t.Errorf("assistant role = %v", msg["role"])
	}
	contentVal, hasContent := msg["content"]
	if !hasContent || contentVal != "" {
		t.Errorf("assistant with tool_calls should have empty string content, got %v", contentVal)
	}
	tc, ok := msg["tool_calls"].([]any)
	if !ok || len(tc) != 1 {
		t.Fatalf("tool_calls = %v", msg["tool_calls"])
	}
	tcObj, ok := tc[0].(map[string]any)
	if !ok {
		t.Fatalf("tool_calls[0] is not an object: %T", tc[0])
	}
	if tcObj["id"] != "call_abc" {
		t.Errorf("tool_call id = %v", tcObj["id"])
	}
	fn, ok := tcObj["function"].(map[string]any)
	if !ok {
		t.Fatalf("tool_call.function is not an object: %T", tcObj["function"])
	}
	if fn["name"] != "get_weather" {
		t.Errorf("tool_call function name = %v", fn["name"])
	}
}

func assertMarshaledToolResultMessage(t *testing.T, msg map[string]any) {
	t.Helper()
	if msg["role"] != "tool" {
		t.Errorf("tool role = %v", msg["role"])
	}
	if msg["tool_call_id"] != "call_abc" {
		t.Errorf("tool_call_id = %v", msg["tool_call_id"])
	}
	if _, hasToolCalls := msg["tool_calls"]; hasToolCalls {
		t.Error("tool message should omit tool_calls")
	}
}

func assertMarshaledFinalAssistantMessage(t *testing.T, msg map[string]any) {
	t.Helper()
	if msg["role"] != "assistant" || msg["content"] != "It's sunny and 72°F in Boston." {
		t.Errorf("final assistant message = %v", msg)
	}
}
