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
// and blocks the task. Not yet wired; see T-041.
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
	// The fields below pick the model. Each stage keeps the tier it runs at
	// today.
	AgentID   string
	Provider  string
	Model     string
	MaxTokens int
	// Attrs are the task facts routing rules match. Zero for a call with no task.
	Attrs spec.TaskAttrs
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
// the caller unmarshals into its own type. TokenUsage and UsageDetails count
// every attempt and are set when Call fails too, so a caller that records
// tokens records what a failed call cost. The agent itself records nothing.
type StageResult struct {
	Outcome      Outcome
	Output       json.RawMessage
	TokenUsage   int
	UsageDetails spec.UsageDetails
}

// ErrNeedsHuman is returned by a caller that handed a NeedsHuman stage result to
// a person. The task is blocked on that question, so the caller stops its work
// on the task rather than continuing without the stage's answer.
var ErrNeedsHuman = errors.New("stage: needs a human decision")

// ErrHumanHandoffFailed is returned when a NeedsHuman result could not be
// recorded for a person. It does not match ErrNeedsHuman, so callers treat
// the task as failed instead of blocked.
var ErrHumanHandoffFailed = errors.New("stage: human handoff failed")

// Agent runs a stage. A ModelAgent is one gateway call; other kinds may use
// tools and the workspace.
type Agent interface {
	Call(ctx context.Context, req StageRequest) (StageResult, error)
}
