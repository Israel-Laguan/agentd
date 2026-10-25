package routing

import (
	"context"

	"agentd/internal/gateway/spec"
	"agentd/internal/stage"
	"agentd/internal/stage/intake"
)

// AnalyzeScope implements spec.AIGateway. The prompt and the post-call check
// live in the intake stage; this method is dead code kept until PR 10.
func (r *Router) AnalyzeScope(ctx context.Context, userIntent string) (*spec.ScopeAnalysis, error) {
	return intake.AnalyzeScope(ctx, stage.NewModelAgent(r), userIntent)
}
