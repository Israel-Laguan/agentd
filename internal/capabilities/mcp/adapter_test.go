package mcp

import (
	"testing"

	"agentd/internal/gateway"

	"github.com/stretchr/testify/assert"
)

func TestConvertInputSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		expected *gateway.FunctionParameters
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
		{
			name:     "non-map input",
			input:    "invalid",
			expected: nil,
		},
		{
			name: "map with properties and required",
			input: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repo":  map[string]any{"type": "string"},
					"issue": map[string]any{"type": "integer"},
				},
				"required": []any{"repo"},
			},
			expected: &gateway.FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"repo":  map[string]any{"type": "string"},
					"issue": map[string]any{"type": "integer"},
				},
				Required: []string{"repo"},
			},
		},
		{
			name: "map without properties",
			input: map[string]any{
				"type": "object",
			},
			expected: &gateway.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		{
			name: "map with empty properties",
			input: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			expected: &gateway.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
			},
		},
		{
			name: "map with required but not string",
			input: map[string]any{
				"type":     "object",
				"required": []any{123, 456},
			},
			expected: &gateway.FunctionParameters{
				Type:       "object",
				Properties: map[string]any{},
				Required:   nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertInputSchema(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, tt.expected.Type, result.Type)
				assert.Equal(t, tt.expected.Properties, result.Properties)
				assert.Equal(t, tt.expected.Required, result.Required)
			}
		})
	}
}
