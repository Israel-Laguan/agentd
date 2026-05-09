package routing

import (
	"context"
	"fmt"

	"agentd/internal/gateway/correction"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

// GeneratePlan implements spec.AIGateway.
func (r *Router) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	req := spec.AIRequest{
		Messages: []spec.PromptMessage{
			{Role: "system", Content: phasePlanningPrompt(r.maxTasksPerPhase)},
			{Role: "user", Content: userIntent},
		},
		Temperature: 0.2,
		JSONMode:    true,
		Role:        spec.RoleChat,
	}
	plan, err := correction.GenerateJSON[models.DraftPlan](ctx, r, req)
	if err != nil {
		return nil, err
	}
	plan = correction.EnforcePhaseCap(plan, r.maxTasksPerPhase)
	return &plan, nil
}

func phasePlanningPrompt(maxTasksPerPhase int) string {
	if maxTasksPerPhase <= 0 {
		return "Create an agentd DraftPlan. Output ONLY valid JSON."
	}
	return fmt.Sprintf(
		"Create an agentd DraftPlan. Output ONLY valid JSON. Emit at most %d tasks. If more work remains, make the last task titled %q with a description summarizing remaining work.",
		maxTasksPerPhase,
		"Plan Phase 2",
	)
}
