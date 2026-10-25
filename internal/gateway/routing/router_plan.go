package routing

import (
	"context"

	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/stage/intake"
)

// GeneratePlan implements spec.AIGateway. The prompt and the phase cap live in
// the intake stage; this method is dead code kept until PR 10.
func (r *Router) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return intake.Plan(ctx, stage.NewModelAgent(r), planPurpose(ctx), r.maxTasksPerPhase, userIntent)
}

// planPurpose lets callers of GeneratePlan (which has no purpose argument) say
// which plan they ask for through the context; the default is a chat plan.
func planPurpose(ctx context.Context) spec.Purpose {
	if p := spec.PurposeFromContext(ctx); p != "" {
		return p
	}
	return spec.PurposeIntakeDraftPlan
}
