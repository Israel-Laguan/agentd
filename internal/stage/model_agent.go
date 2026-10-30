package stage

import (
	"context"
	"encoding/json"
	"errors"

	"agentd/internal/gateway/spec"
)

// ErrModelAgentAccess is returned when a stage asks a ModelAgent for workspace
// access. A model call has no tools, so only AccessNone is allowed.
var ErrModelAgentAccess = errors.New("stage: model agent cannot give workspace access")

// ModelAgent runs a stage as one gateway call with no tools. Cheap stages use
// it so they stay cheap.
type ModelAgent struct {
	gateway spec.AIGateway
}

// NewModelAgent returns a ModelAgent that sends its calls through gw.
func NewModelAgent(gw spec.AIGateway) ModelAgent {
	return ModelAgent{gateway: gw}
}

// Call makes one gateway call and returns the validated JSON. A contract
// violation is retried by the Contract itself, the same corrective retry every
// JSON call already makes.
func (a ModelAgent) Call(ctx context.Context, req StageRequest) (StageResult, error) {
	if err := req.Validate(); err != nil {
		return StageResult{}, err
	}
	if req.Access != AccessNone {
		return StageResult{}, ErrModelAgentAccess
	}
	aiReq := spec.AIRequest{
		Purpose:     req.Purpose,
		TaskID:      spec.TaskIDFromContext(ctx),
		Messages:    req.Context,
		Temperature: req.Temperature,
		AgentID:     req.AgentID,
		Provider:    req.Provider,
		Model:       req.Model,
		MaxTokens:   req.MaxTokens,
		Attrs:       req.Attrs,
	}
	output, tokens, details, err := req.Contract.Decode(ctx, a.gateway, aiReq)
	if err != nil {
		return StageResult{TokenUsage: tokens, UsageDetails: details}, err
	}
	outcome := OutcomeCompleted
	// A decoded response may mark itself as needing a human decision even though
	// the model call itself succeeded; map that marker to OutcomeNeedsHuman so
	// callers can hand it to a person.
	var marker struct {
		NeedsHuman bool `json:"needs_human"`
	}
	if json.Unmarshal(output, &marker) == nil && marker.NeedsHuman {
		outcome = OutcomeNeedsHuman
	}
	return StageResult{
		Outcome:      outcome,
		Output:       output,
		TokenUsage:   tokens,
		UsageDetails: details,
	}, nil
}
