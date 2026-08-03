package gateway

import (
	"encoding/json"
	"sort"
)

// SortTools orders a ToolDefinition slice canonically by Name so the serialized
// tools block is byte-stable across requests (cache stable-prefix). Tools with
// the same Name are ordered by their full JSON representation to guarantee
// deterministic output. Sorts in place and returns the same slice for convenience.
func SortTools(tools []ToolDefinition) []ToolDefinition {
	sort.SliceStable(tools, func(i, j int) bool {
		if tools[i].Name != tools[j].Name {
			return tools[i].Name < tools[j].Name
		}
		// Deterministic secondary key: canonical JSON of the complete definition.
		bi, _ := json.Marshal(tools[i])
		bj, _ := json.Marshal(tools[j])
		return string(bi) < string(bj)
	})
	return tools
}
