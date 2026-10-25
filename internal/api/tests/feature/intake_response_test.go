package api_test

import (
	"context"
	"encoding/json"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// intakeResponse answers an intake purpose by calling the matching fake method
// and returning its result as JSON. The message the stage sends is the intent.
func intakeResponse(
	ctx context.Context,
	req gateway.AIRequest,
	classify func(context.Context, string) (*gateway.IntentAnalysis, error),
	scope func(context.Context, string) (*gateway.ScopeAnalysis, error),
	plan func(context.Context, string) (*models.DraftPlan, error),
) (gateway.AIResponse, error) {
	var intent string
	if n := len(req.Messages); n > 0 {
		intent = req.Messages[n-1].Content
	}
	var (
		v   any
		err error
	)
	switch req.Purpose {
	case gateway.PurposeIntakeClassifyIntent:
		v, err = classify(ctx, intent)
	case gateway.PurposeIntakeAnalyzeScope:
		v, err = scope(ctx, intent)
	case gateway.PurposeIntakeDraftPlan, gateway.PurposeCreateReplanComment:
		v, err = plan(ctx, intent)
	default:
		return gateway.AIResponse{}, nil
	}
	if err != nil {
		return gateway.AIResponse{}, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return gateway.AIResponse{}, err
	}
	return gateway.AIResponse{Content: string(raw)}, nil
}
