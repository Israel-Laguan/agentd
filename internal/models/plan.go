package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// DraftPlan is the Frontdesk output awaiting human approval.
type DraftPlan struct {
	ProjectName string      `json:"project_name"`
	Description string      `json:"description,omitempty"`
	Tasks       []DraftTask `json:"tasks"`
	// ClarificationQuestion is set instead of tasks when the request names no
	// target or outcome to plan. The planner returns it to the user (B-085).
	ClarificationQuestion string `json:"clarification_question,omitempty"`
	// SourcePath is an optional local filesystem path whose contents are
	// copied into the project workspace before tasks become claimable.
	// When set, the server performs an atomic copy so workers never run
	// against an empty directory.
	SourcePath          string `json:"source_path,omitempty"`
	StartEmptyWorkspace bool   `json:"start_empty_workspace,omitempty"`
	// WorkspacePending when true forces all root tasks (those without
	// dependencies) into PENDING state at creation time. Used by the
	// ProjectService to defer task dispatch until workspace content is
	// ready. Not serialized in the API response.
	WorkspacePending bool `json:"-"`
	// UserID is the identity the plan is being materialized for, carried
	// from the originating chat turn's X-Agentd-User header. It is stamped on
	// the project so execution-time memory recall can scope that user's saved
	// preferences. Optional: blank means no preferences are recalled.
	UserID string `json:"user_id,omitempty"`
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

// Host system paths in task text are only a defect when a worker is told to
// read them: /proc and /sys exist only on the host, and any other system path
// preceded by a read verb is a host read. Paths named as project content (a
// Dockerfile WORKDIR /usr/src/app, an nginx root /var/www/html) are fine.
// Workers run in a jailed workspace, so a host read can never succeed
// (/dev/null, the one allowed absolute path, is not matched).
const systemPathBody = `/(?:etc|proc|sys|root|home|var|usr|boot|opt|mnt|srv|lib|bin|sbin)(?:/[^\s"'),;]*)?`

var (
	virtualFSPathPattern = regexp.MustCompile(`(?:^|[\s"'(=:,])/(?:proc|sys)(?:/[^\s"'),;]*)?`)
	hostReadPathPattern  = regexp.MustCompile(`(?i)\b(?:cat|read|reads|reading|less|head|tail|ls|grep|open|view|print|parse|inspect|dump|list|show)\b[^\n/]{0,40}(?:^|[\s"'(=:,])(` + systemPathBody + `)`)
)

// hostReadPath returns the host path a task tells its worker to read, or "".
func hostReadPath(text string) string {
	if m := virtualFSPathPattern.FindString(text); m != "" {
		return strings.TrimRight(strings.TrimLeft(m, " \t\n\"'(=:,"), ".:")
	}
	if m := hostReadPathPattern.FindStringSubmatch(text); m != nil {
		return strings.TrimRight(m[1], ".:")
	}
	return ""
}

// Validate is invoked by gateway.GenerateJSON before returning a DraftPlan to
// the caller, so structural errors trigger a corrective LLM retry instead of
// bubbling up to the human approval step. It also normalizes the plan: a task
// that reads a file another task writes gains the depends_on edge the model
// left out (see ensureFileDependencies).
func (p *DraftPlan) Validate() error {
	if strings.TrimSpace(p.ClarificationQuestion) != "" {
		p.ClarificationQuestion = strings.TrimSpace(p.ClarificationQuestion)
		p.Tasks = nil
		return nil
	}
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
		if path := hostReadPath(t.Title + "\n" + t.Description); path != "" {
			return fmt.Errorf("task %d: reads %q, which is outside the project workspace and unreadable to workers; use commands such as hostnamectl or lscpu instead of system files", i+1, path)
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
	return p.ensureFileDependencies()
}

// DraftTask is a plan-local task. TempID values are resolved to UUIDs when the
// plan is materialized.
type DraftTask struct {
	// ReferenceID is the proposal-aligned ID and maps to JSON ref_id.
	ReferenceID string `json:"ref_id,omitempty"`
	// TempID remains for backward compatibility with existing tests and stores.
	TempID      string       `json:"temp_id,omitempty"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Assignee    TaskAssignee `json:"assignee"`
	// AgentID optionally pre-assigns the task to a specific agent profile at
	// creation time. When non-empty the materialized task row carries this
	// agent_id, eliminating the race between dispatch and a separate assign
	// call. Empty means the store default ("default") is used.
	AgentID         string   `json:"agent_id,omitempty"`
	DependsOn       []string `json:"depends_on,omitempty"`
	SuccessCriteria []string `json:"success_criteria,omitempty"`
	// RequiresWorkspaceChange marks a task that must change the workspace.
	RequiresWorkspaceChange bool `json:"requires_workspace_change,omitempty"`
	// SplitChild marks a draft created by the split stage, so the store links
	// it to its parent with a SPLIT relation instead of an ordinary BLOCKS
	// dependency. It is never read from plan JSON.
	SplitChild bool `json:"-"`
}

// UnmarshalJSON accepts both proposal snake_case and legacy camel-case keys.
func (d *DraftTask) UnmarshalJSON(data []byte) error {
	type alias DraftTask
	var raw struct {
		alias
		TempIDLegacy          string       `json:"TempID"`
		ReferenceIDLegacy     string       `json:"ReferenceID"`
		TitleLegacy           string       `json:"Title"`
		DescriptionLegacy     string       `json:"Description"`
		AssigneeLegacy        TaskAssignee `json:"Assignee"`
		AgentIDLegacy         string       `json:"AgentID"`
		DependsOnLegacy       []string     `json:"DependsOn"`
		SuccessCriteriaLegacy []string     `json:"SuccessCriteria"`
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
	if d.AgentID == "" {
		d.AgentID = raw.AgentIDLegacy
	}
	if len(d.DependsOn) == 0 && len(raw.DependsOnLegacy) > 0 {
		d.DependsOn = raw.DependsOnLegacy
	}
	if len(d.SuccessCriteria) == 0 && len(raw.SuccessCriteriaLegacy) > 0 {
		d.SuccessCriteria = append([]string(nil), raw.SuccessCriteriaLegacy...)
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

// TieredDAGTask describes one child task in a tiered execution DAG for
// PersistTieredDAG. DependsOnID is empty for the first step (no predecessor).
type TieredDAGTask struct {
	Task        Task
	DependsOnID string
}

// TieredContinuationTask describes one child task spawned onto an
// already-BLOCKED tiered pipeline origin (mid-fix redo, escalation, or a
// NEEDS_CONTEXT re-gather step). Unlike PersistTieredDAG, the origin task's
// own state is left untouched, since it is already BLOCKED for the
// pipeline's duration. DependsOnID is empty when the task should start READY.
type TieredContinuationTask struct {
	Task        Task
	DependsOnID string
	// IdempotencyKey dedups the whole continuation batch on retry: a matching
	// key returns all originally spawned children and inserts nothing new.
	// Callers should use a key that uniquely identifies the batch, not an
	// individual child's key, since a matching key returns every child in
	// the batch.
	IdempotencyKey string
}
