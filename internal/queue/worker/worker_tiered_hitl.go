package worker

import (
	"context"
	"log/slog"

	"agentd/internal/models"
)

// taskBlockedOnHuman reports whether task is parked on a human answer (a
// clarification subtask is open or the task itself is BLOCKED), so a tiered
// step that exited without a LoopResult because of a HITL handoff must not be
// treated as a failure. When neither the task nor its children can be read the
// answer is unknown, and it reports true: failing a task that may be waiting on
// a person is worse than leaving it for the next pass to settle.
func (w *Worker) taskBlockedOnHuman(ctx context.Context, task models.Task) bool {
	current, taskErr := w.store.GetTask(ctx, task.ID)
	if taskErr == nil && current != nil && current.State == models.TaskStateBlocked {
		return true
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		if taskErr != nil {
			slog.Warn("HITL check inconclusive; keeping the task", "task_id", task.ID, "task_error", taskErr, "children_error", err)
			return true
		}
		return false
	}
	return findPendingClarificationSubtask(children) != nil
}
