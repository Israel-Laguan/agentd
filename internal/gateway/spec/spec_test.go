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
	if parsed[0]["role"] != "system" || parsed[1]["role"] != "user" {
		t.Errorf("system/user messages = %v, %v", parsed[0], parsed[1])
	}
	assistantWithToolCalls := parsed[2]
	if assistantWithToolCalls["role"] != "assistant" {
		t.Errorf("assistant role = %v", assistantWithToolCalls["role"])
	}
	tc, ok := assistantWithToolCalls["tool_calls"].([]any)
	if !ok || len(tc) != 1 {
		t.Fatalf("tool_calls = %v", assistantWithToolCalls["tool_calls"])
	}
	tcObj := tc[0].(map[string]any)
	if tcObj["id"] != "call_abc" {
		t.Errorf("tool_call id = %v", tcObj["id"])
	}
	if fn := tcObj["function"].(map[string]any); fn["name"] != "get_weather" {
		t.Errorf("tool_call function name = %v", fn["name"])
	}
	toolMsg := parsed[3]
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_abc" {
		t.Errorf("tool message = %v", toolMsg)
	}
	if parsed[4]["content"] != "It's sunny and 72°F in Boston." {
		t.Errorf("final assistant message = %v", parsed[4])
	}
}
