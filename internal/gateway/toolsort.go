package gateway

import "sort"

// SortTools orders a ToolDefinition slice canonically by Name so the serialized
// tools block is byte-stable across requests (cache stable-prefix). Sorts in
// place and returns the same slice for convenience.
func SortTools(tools []ToolDefinition) []ToolDefinition {
	sort.SliceStable(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	return tools
}
