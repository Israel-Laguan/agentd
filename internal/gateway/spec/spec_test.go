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

func TestPromptMessage_MarshalToolConversation(t *testing.T) {
	messages := []PromptMessage{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What's the weather?"},
		{
			Role:      "assistant",
			ToolCalls: []ToolCall{{ID: "call_abc", Type: "function", Function: ToolCallFunction{Name: "get_weather", Arguments: `{"location":"Boston"}`}}},
		},
		{
			Role:       "tool",
			ToolCallID: "call_abc",
			Content:    `{"temp":72,"conditions":"sunny"}`,
		},
		{Role: "assistant", Content: "It's sunny and 72°F in Boston."},
	}

	data, err := json.Marshal(messages)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var parsed []map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if len(parsed) != 5 {
		t.Fatalf("len(messages) = %d, want 5", len(parsed))
	}

	if parsed[0]["role"] != "system" || parsed[0]["content"] != "You are a helpful assistant." {
		t.Errorf("system message = %v", parsed[0])
	}
	if parsed[1]["role"] != "user" || parsed[1]["content"] != "What's the weather?" {
		t.Errorf("user message = %v", parsed[1])
	}

	assistantWithToolCalls := parsed[2]
	if assistantWithToolCalls["role"] != "assistant" {
		t.Errorf("assistant role = %v", assistantWithToolCalls["role"])
	}
	contentVal, hasContent := assistantWithToolCalls["content"]
	if !hasContent || contentVal != "" {
		t.Errorf("assistant with tool_calls should have empty string content, got %v", contentVal)
	}
	tc, ok := assistantWithToolCalls["tool_calls"].([]any)
	if !ok || len(tc) != 1 {
		t.Fatalf("tool_calls = %v", assistantWithToolCalls["tool_calls"])
	}
	tcObj := tc[0].(map[string]any)
	if tcObj["id"] != "call_abc" {
		t.Errorf("tool_call id = %v", tcObj["id"])
	}
	fn := tcObj["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tool_call function name = %v", fn["name"])
	}

	toolMsg := parsed[3]
	if toolMsg["role"] != "tool" {
		t.Errorf("tool role = %v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_abc" {
		t.Errorf("tool_call_id = %v", toolMsg["tool_call_id"])
	}
	if _, hasToolCalls := toolMsg["tool_calls"]; hasToolCalls {
		t.Error("tool message should omit tool_calls")
	}

	if parsed[4]["role"] != "assistant" || parsed[4]["content"] != "It's sunny and 72°F in Boston." {
		t.Errorf("final assistant message = %v", parsed[4])
	}
}
