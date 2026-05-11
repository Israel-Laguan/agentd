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
