package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// breakdownSubtasksPrefix marks a parent comment listing the subtask IDs a
// worker breakdown created. The kanban returns a BLOCKED parent to READY once
// every child completes; without this marker the resumed parent would be
// re-prompted with the identical task and could be decomposed again forever.
const breakdownSubtasksPrefix = "[worker-breakdown] subtasks="

// recordBreakdownSubtasks stores the breakdown marker on the parent. A failure
// only loses the roll-up on resume, so it is logged rather than surfaced.
func (w *Worker) recordBreakdownSubtasks(ctx context.Context, parent models.Task, children []models.Task) {
	if len(children) == 0 {
		return
	}
	ids := make([]string, len(children))
	for i, child := range children {
		ids[i] = child.ID
	}
	if err := w.store.AddComment(ctx, models.Comment{
		TaskID: parent.ID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   breakdownSubtasksPrefix + strings.Join(ids, ","),
	}); err != nil {
		slog.Error("failed to record breakdown subtasks; parent will be re-prompted on resume",
			"task_id", parent.ID, "error", err)
	}
}

// latestBreakdownSubtaskIDs returns the subtask IDs from the most recent
// breakdown marker, or nil when the task was never broken down.
func latestBreakdownSubtaskIDs(comments []models.Comment) []string {
	var ids []string
	var latest models.Comment
	found := false
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, breakdownSubtasksPrefix) {
			continue
		}
		if found && c.CreatedAt.Before(latest.CreatedAt) {
			continue
		}
		latest, found = c, true
		ids = nil
		for _, id := range strings.Split(strings.TrimPrefix(c.Body, breakdownSubtasksPrefix), ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// tryRollUpBreakdown completes a resumed parent whose breakdown subtasks have
// all completed, instead of asking the model to redo the decomposed work. It
// reports whether the task was handled.
func (w *Worker) tryRollUpBreakdown(ctx context.Context, task models.Task) bool {
	comments, err := w.store.ListComments(ctx, task.ID)
	if err != nil {
		slog.Warn("breakdown roll-up: list comments failed", "task_id", task.ID, "error", err)
		return false
	}
	ids := latestBreakdownSubtaskIDs(comments)
	if len(ids) == 0 {
		return false
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		slog.Warn("breakdown roll-up: list children failed", "task_id", task.ID, "error", err)
		return false
	}
	states := make(map[string]models.TaskState, len(children))
	for _, child := range children {
		states[child.ID] = child.State
	}
	for _, id := range ids {
		if states[id] != models.TaskStateCompleted {
			return false
		}
	}
	summary := fmt.Sprintf("completed via %d breakdown subtasks: %s", len(ids), strings.Join(ids, ", "))
	if w.commitSucceeded(ctx, task, sandbox.Result{Success: true, Stdout: summary}, nil) {
		w.Emit(ctx, task, "TASK_BREAKDOWN_ROLLUP", summary)
	}
	return true
}
