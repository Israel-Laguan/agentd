package worker

import (
	"context"

	"agentd/internal/models"
)

// taskBlockedOnHuman reports whether task is parked on a human answer (a
// clarification subtask is open or the task itself is BLOCKED), so a tiered
// step that exited without a LoopResult because of a HITL handoff must not be
// treated as a failure.
func (w *Worker) taskBlockedOnHuman(ctx context.Context, task models.Task) bool {
	if current, err := w.store.GetTask(ctx, task.ID); err == nil && current != nil && current.State == models.TaskStateBlocked {
		return true
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		return false
	}
	return findPendingClarificationSubtask(children) != nil
}

func workerBoolPtr(b bool) *bool {
	v := b
	out := &v
	return out
}
