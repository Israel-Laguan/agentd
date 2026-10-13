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

// hostFactPlanningRule keeps plans about the machine itself executable: tasks run
// as shell commands in a jailed workspace, so facts must come from commands.
const hostFactPlanningRule = "\nTasks run as shell commands inside the project workspace and cannot read files outside it, so never name /etc, /proc or /sys paths in task descriptions. " +
	"When the request asks about this machine's OS or hardware, gather the facts with commands such as uname -a, hostnamectl, lscpu, free -h, lsblk and df -h, " +
	"saving each output to a file, and finish with a task that builds report.md in the workspace from those files; never state facts you did not collect."

func phasePlanningPrompt(maxTasksPerPhase int) string {
	base := "Create an agentd DraftPlan. Output ONLY valid JSON.\n" + models.DraftPlan{}.SchemaHint() + hostFactPlanningRule
	if maxTasksPerPhase <= 0 {
		return base
	}
	return base + fmt.Sprintf(
		"\nEmit at most %d tasks. If more work remains, make the last task titled %q with a description summarizing remaining work.",
		maxTasksPerPhase,
		"Plan Phase 2",
	)
}
