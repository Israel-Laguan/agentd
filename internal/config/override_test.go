package config

import (
	"testing"
)

func TestFlattenYAML(t *testing.T) {
	input := map[string]any{
		"a": map[string]any{
			"b": "value",
			"c": map[string]any{
				"d": "nested",
			},
		},
		"e": "simple",
	}
	output := map[string]any{}
	flattenYAML("", input, output)

	if output["a.b"] != "value" {
		t.Errorf("a.b = %v, want value", output["a.b"])
	}
	if output["a.c.d"] != "nested" {
		t.Errorf("a.c.d = %v, want nested", output["a.c.d"])
	}
	if output["e"] != "simple" {
		t.Errorf("e = %v, want simple", output["e"])
	}
}

func TestFlattenYAML_WithPrefix(t *testing.T) {
	input := map[string]any{
		"b": "value",
	}
	output := map[string]any{}
	flattenYAML("prefix", input, output)

	if output["prefix.b"] != "value" {
		t.Errorf("prefix.b = %v, want value", output["prefix.b"])
	}
}

func TestFlattenYAML_Nested(t *testing.T) {
	input := map[string]any{
		"top": map[string]any{
			"mid": map[string]any{
				"bottom": "deep",
			},
		},
	}
	output := map[string]any{}
	flattenYAML("", input, output)

	if output["top.mid.bottom"] != "deep" {
		t.Errorf("top.mid.bottom = %v, want deep", output["top.mid.bottom"])
	}
}