package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GoldenCriterion names a success criterion and the controller's check for it.
type GoldenCriterion struct {
	Criterion    string `json:"criterion"`
	CheckCommand string `json:"check_command"`
}

// UnmarshalJSON accepts snake_case and legacy camel-case keys.
func (g *GoldenCriterion) UnmarshalJSON(data []byte) error {
	type alias GoldenCriterion
	var raw struct {
		alias
		CriterionLegacy    string `json:"Criterion"`
		CheckCommandLegacy string `json:"CheckCommand"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*g = GoldenCriterion(raw.alias)
	if g.Criterion == "" {
		g.Criterion = raw.CriterionLegacy
	}
	if g.CheckCommand == "" {
		g.CheckCommand = raw.CheckCommandLegacy
	}
	return nil
}

// ValidateGoldenCriteria rejects missing commands, unknown criteria, and
// duplicate checks, which would otherwise give a misleading progress count.
func ValidateGoldenCriteria(criteria []string, golden []GoldenCriterion) error {
	allowed := make(map[string]bool, len(criteria))
	for _, c := range criteria {
		allowed[c] = true
	}
	seen := make(map[string]bool, len(golden))
	for _, g := range golden {
		if strings.TrimSpace(g.Criterion) == "" || !allowed[g.Criterion] {
			return fmt.Errorf("golden criterion %q must name a success criterion", g.Criterion)
		}
		if strings.TrimSpace(g.CheckCommand) == "" {
			return fmt.Errorf("golden criterion %q requires a check command", g.Criterion)
		}
		if seen[g.Criterion] {
			return fmt.Errorf("duplicate golden criterion %q", g.Criterion)
		}
		seen[g.Criterion] = true
	}
	return nil
}
