package routing

import (
	"context"

	"agentd/internal/gateway/spec"
	"agentd/internal/stage"
	"agentd/internal/stage/intake"
)

// ClassifyIntent implements spec.AIGateway. The prompt and the post-call check
// live in the intake stage; this method is dead code kept until PR 10.
func (r *Router) ClassifyIntent(ctx context.Context, userIntent string) (*spec.IntentAnalysis, error) {
	return intake.ClassifyIntent(ctx, stage.NewModelAgent(r), userIntent)
}
