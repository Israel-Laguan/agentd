package context

import (
	"fmt"
	"regexp"
	"strings"
)

var stepIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// PlanStep is one numbered step in a structured work plan.
type PlanStep struct {
	ID           string            `json:"id"`
	Action       string            `json:"action"`
	Inputs       map[string]string `json:"inputs"`
	OutputFormat string            `json:"output_format"`
}

// Plan is the JSON work plan produced before agentic execution.
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// Validate checks semantic constraints on a parsed plan.
func (p *Plan) Validate() error {
	if p == nil || len(p.Steps) == 0 {
		return fmt.Errorf("plan must contain at least one step")
	}
	seen := make(map[string]struct{}, len(p.Steps))
	for i, step := range p.Steps {
		id := strings.TrimSpace(step.ID)
		if id == "" {
			return fmt.Errorf("step %d: id is required", i+1)
		}
		if !stepIDPattern.MatchString(id) {
			return fmt.Errorf("step %q: id must match %q", id, stepIDPattern.String())
		}
		action := strings.TrimSpace(step.Action)
		if action == "" {
			return fmt.Errorf("step %q: action is required", id)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("duplicate step id %q", id)
		}
		p.Steps[i].ID = id
		p.Steps[i].Action = action
		seen[id] = struct{}{}
	}
	return nil
}
