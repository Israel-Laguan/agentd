package intake

import (
	"fmt"
	"strings"

	"agentd/internal/models"
)

// enforcePhaseCap trims an oversized DraftPlan to the configured phase cap: the
// tasks past the cap become one "Plan Phase 2" task that plans them later.
func enforcePhaseCap(plan models.DraftPlan, maxTasksPerPhase int) models.DraftPlan {
	if maxTasksPerPhase <= 0 || len(plan.Tasks) <= maxTasksPerPhase {
		return plan
	}
	// A cap of 1 cannot hold both an executable task and its continuation.
	// Keeping only the continuation would make the next phase plan nothing but
	// another continuation, forever, so the floor is 2. Config rejects a
	// positive cap below 2 as well; this keeps a direct caller from looping.
	if maxTasksPerPhase < 2 {
		maxTasksPerPhase = 2
	}
	remaining := append([]models.DraftTask(nil), plan.Tasks[maxTasksPerPhase-1:]...)
	plan.Tasks = append([]models.DraftTask(nil), plan.Tasks[:maxTasksPerPhase-1]...)
	plan.Tasks = append(plan.Tasks, phaseContinuationTask(2, remaining))
	return plan
}

func phaseContinuationTask(phase int, remaining []models.DraftTask) models.DraftTask {
	return models.DraftTask{
		Title:       fmt.Sprintf("Plan Phase %d", phase),
		Description: phaseContinuationDescription(remaining),
		Assignee:    models.TaskAssigneeSystem,
	}
}

func phaseContinuationDescription(remaining []models.DraftTask) string {
	if len(remaining) == 0 {
		return "Continue planning the remaining work for the project."
	}
	var b strings.Builder
	b.WriteString("Continue planning the remaining work. Start from these deferred tasks and any related follow-up work:\n")
	for _, task := range remaining {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = strings.TrimSpace(task.ID())
		}
		if title == "" {
			continue
		}
		b.WriteString("- ")
		b.WriteString(title)
		description := strings.TrimSpace(task.Description)
		if description != "" {
			b.WriteString(": ")
			b.WriteString(description)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}
