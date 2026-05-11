package spec

import (
	"encoding/json"
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

			want := `{"type":"object","properties":{},"required":[],"additionalProperties":false}`
			if string(data) != want {
				t.Fatalf("Marshal() = %s, want %s", data, want)
			}
		})
	}
}

func TestToolDefinitionMarshalJSON_OmitsNilParameters(t *testing.T) {
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
	if _, ok := got["parameters"]; ok {
		t.Fatalf("parameters present in JSON: %s", data)
	}
}
