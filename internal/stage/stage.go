// Package stage is the seam between agentd and the agents it asks for work.
// Each stage makes one Agent.Call with its prompt, the JSON contract it must
// satisfy and the workspace access it may use. The core applies the result.
package stage

import (
	"context"
	"encoding/json"
	"errors"

	"agentd/internal/gateway/spec"
)

// Stage names where in agentd's pipeline a call belongs.
type Stage string

const (
	StageIntake       Stage = "intake"
	StageTaskCreation Stage = "task_creation"
	StageExecution    Stage = "execution"
)

// WorkspaceAccess is what a stage may do to the project workspace. The zero
// value is AccessNone, so a stage gets no access unless it asks for it.
type WorkspaceAccess int

const (
	AccessNone WorkspaceAccess = iota
	AccessReadOnly
	AccessWrite
)

// Outcome is how a stage ended.
//
// OutcomeNeedsHuman means the stage cannot finish without a person. It is
// meant to land in Worker.AskHuman (hitl_tasks.go), which records the question
// and blocks the task. PR 11 wires that path, so no second suspend path exists.
type Outcome string

const (
	OutcomeCompleted  Outcome = "completed"
	OutcomeNeedsHuman Outcome = "needs_human"
)

// StageRequest is one call to an Agent. Purpose is explicit, not read from
// ctx, so two stages that build the same prompt can still be told apart.
// The task ID still travels in ctx (spec.TaskIDFromContext).
type StageRequest struct {
	Stage    Stage
	Purpose  spec.Purpose
	Context  []spec.PromptMessage
	Contract Contract
	Access   WorkspaceAccess
	// Temperature is the sampling temperature for this call. Each stage keeps
	// the value its prompt was tuned with.
	Temperature float64
}

// Validate reports a request that is missing what every call needs.
func (r StageRequest) Validate() error {
	switch {
	case r.Stage == "":
		return errors.New("stage: stage is required")
	case r.Purpose == "":
		return errors.New("stage: purpose is required")
	case r.Contract.decode == nil:
		return errors.New("stage: contract is required")
	}
	return nil
}

// StageResult is what a stage returned. Output is the validated JSON, which
// the caller unmarshals into its own type.
type StageResult struct {
	Outcome      Outcome
	Output       json.RawMessage
	TokenUsage   int
	UsageDetails spec.UsageDetails
}

// Agent runs a stage. A ModelAgent is one gateway call; other kinds may use
// tools and the workspace.
type Agent interface {
	Call(ctx context.Context, req StageRequest) (StageResult, error)
}
