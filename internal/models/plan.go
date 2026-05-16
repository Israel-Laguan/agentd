package models

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DraftPlan is the Frontdesk output awaiting human approval.
type DraftPlan struct {
	ProjectName string      `json:"project_name"`
	Description string      `json:"description,omitempty"`
	Tasks       []DraftTask `json:"tasks"`
}

// UnmarshalJSON accepts both proposal snake_case and legacy camel-case keys.
func (p *DraftPlan) UnmarshalJSON(data []byte) error {
	type alias DraftPlan
	var raw struct {
		alias
		ProjectNameLegacy string      `json:"ProjectName"`
		DescriptionLegacy string      `json:"Description"`
		TasksLegacy       []DraftTask `json:"Tasks"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = DraftPlan(raw.alias)
	if p.ProjectName == "" {
		p.ProjectName = raw.ProjectNameLegacy
	}
	if p.Description == "" {
		p.Description = raw.DescriptionLegacy
	}
	if len(p.Tasks) == 0 && len(raw.TasksLegacy) > 0 {
		p.Tasks = raw.TasksLegacy
	}
	return nil
}

// Validate is invoked by gateway.GenerateJSON before returning a DraftPlan to
// the caller, so structural errors trigger a corrective LLM retry instead of
// bubbling up to the human approval step.
func (p *DraftPlan) Validate() error {
	if strings.TrimSpace(p.ProjectName) == "" {
		return fmt.Errorf("project name is required")
	}
	if len(p.Tasks) == 0 {
		return fmt.Errorf("plan must have at least one task")
	}
	seen := make(map[string]struct{}, len(p.Tasks))
	for i, t := range p.Tasks {
		tid := t.ID()
		if strings.TrimSpace(t.Title) == "" {
			return fmt.Errorf("task %d: title is required", i+1)
		}
		if tid != "" {
			if _, dup := seen[tid]; dup {
				return fmt.Errorf("duplicate temp_id %q", tid)
			}
			seen[tid] = struct{}{}
		}
	}
	for i, t := range p.Tasks {
		for _, dep := range t.DependsOn {
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf("task %d: depends_on %q not in plan", i+1, dep)
			}
		}
	}
	return nil
}

// DraftTask is a plan-local task. TempID values are resolved to UUIDs when the
// plan is materialized.
type DraftTask struct {
	// ReferenceID is the proposal-aligned ID and maps to JSON ref_id.
	ReferenceID string `json:"ref_id,omitempty"`
	// TempID remains for backward compatibility with existing tests and stores.
	TempID          string       `json:"temp_id,omitempty"`
	Title           string       `json:"title"`
	Description     string       `json:"description"`
	Assignee        TaskAssignee `json:"assignee"`
	DependsOn       []string     `json:"depends_on,omitempty"`
	SuccessCriteria []string     `json:"success_criteria,omitempty"`
}

// UnmarshalJSON accepts both proposal snake_case and legacy camel-case keys.
func (d *DraftTask) UnmarshalJSON(data []byte) error {
	type alias DraftTask
	var raw struct {
		alias
		TempIDLegacy      string       `json:"TempID"`
		ReferenceIDLegacy string       `json:"ReferenceID"`
		TitleLegacy       string       `json:"Title"`
		DescriptionLegacy string       `json:"Description"`
		AssigneeLegacy    TaskAssignee `json:"Assignee"`
		DependsOnLegacy   []string     `json:"DependsOn"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*d = DraftTask(raw.alias)
	if d.ReferenceID == "" {
		d.ReferenceID = raw.ReferenceIDLegacy
	}
	if d.TempID == "" {
		d.TempID = raw.TempIDLegacy
	}
	if d.Title == "" {
		d.Title = raw.TitleLegacy
	}
	if d.Description == "" {
		d.Description = raw.DescriptionLegacy
	}
	if d.Assignee == "" {
		d.Assignee = raw.AssigneeLegacy
	}
	if len(d.DependsOn) == 0 && len(raw.DependsOnLegacy) > 0 {
		d.DependsOn = raw.DependsOnLegacy
	}
	return nil
}

// ID returns the canonical draft-local task identifier.
func (d DraftTask) ID() string {
	if id := strings.TrimSpace(d.ReferenceID); id != "" {
		return id
	}
	return strings.TrimSpace(d.TempID)
}
