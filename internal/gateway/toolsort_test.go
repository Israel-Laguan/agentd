package gateway

import (
	"encoding/json"
	"testing"
)

// TestSortTools_DeterministicSameName verifies that SortTools provides a stable
// secondary ordering for tools sharing the same Name, so the output is
// deterministic regardless of input order.
func TestSortTools_DeterministicSameName(t *testing.T) {
	t.Parallel()

	// Two distinct definitions with the same Name, in opposite order.
	tools := []ToolDefinition{
		{
			Name:        "search",
			Description: "first",
			Parameters: &FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"q": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "search",
			Description: "second",
			Parameters: &FunctionParameters{
				Type: "object",
				Properties: map[string]any{
					"q": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
			},
		},
	}

	got := SortTools(append([]ToolDefinition(nil), tools...))
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Description != "first" || got[1].Description != "second" {
		t.Fatalf("order = [%q %q], want [first second]", got[0].Description, got[1].Description)
	}

	// Reverse the input order and confirm the output is identical.
	reversed := []ToolDefinition{tools[1], tools[0]}
	got2 := SortTools(append([]ToolDefinition(nil), reversed...))
	if len(got2) != 2 {
		t.Fatalf("len = %d, want 2", len(got2))
	}
	if got2[0].Description != "first" || got2[1].Description != "second" {
		t.Fatalf("order = [%q %q], want [first second]", got2[0].Description, got2[1].Description)
	}

	// Confirm both runs produced byte-identical slices.
	bi, _ := json.Marshal(got)
	bj, _ := json.Marshal(got2)
	if string(bi) != string(bj) {
		t.Fatalf("non-deterministic output: %s vs %s", bi, bj)
	}
}
