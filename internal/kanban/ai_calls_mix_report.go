package kanban

import (
	"fmt"
	"strings"

	"agentd/internal/gateway/spec"
)

// aiCutLayers are the names a record's truncated_by can hold; "router" is the
// gateway's own cut.
var aiCutLayers = []string{"working_zone", "strategy_budget", "agentic_head", "router"}

// aiMixColumns sums each section's chars and counts calls cut by each layer.
// Section and layer names are constants, never user input. json_valid guards
// rows written before the mix existed (an empty string).
func aiMixColumns() string {
	var cols []string
	for _, s := range spec.AllSections() {
		cols = append(cols, fmt.Sprintf(
			"COALESCE(SUM(CASE WHEN json_valid(context_mix) THEN json_extract(context_mix, '$.%s.chars') END), 0)", s))
	}
	cols = append(cols, "COALESCE(SUM(truncated_by != ''), 0)")
	for _, l := range aiCutLayers {
		cols = append(cols, fmt.Sprintf("COALESCE(SUM(instr(truncated_by, '%s') > 0), 0)", l))
	}
	return strings.Join(cols, ", ")
}

// mixScan reads the aiMixColumns values into maps after the scan.
type mixScan struct {
	chars []int
	cuts  []int
	any   int
}

func newMixScan() *mixScan {
	return &mixScan{chars: make([]int, len(spec.AllSections())), cuts: make([]int, len(aiCutLayers))}
}

func (m *mixScan) dest() []any {
	out := make([]any, 0, len(m.chars)+len(m.cuts)+1)
	for i := range m.chars {
		out = append(out, &m.chars[i])
	}
	out = append(out, &m.any)
	for i := range m.cuts {
		out = append(out, &m.cuts[i])
	}
	return out
}

func (m *mixScan) fill(chars map[string]int, cuts map[string]int) {
	for i, s := range spec.AllSections() {
		chars[string(s)] = m.chars[i]
	}
	for i, l := range aiCutLayers {
		cuts[l] = m.cuts[i]
	}
}
