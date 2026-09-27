package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// breakdownSubtasksPrefix marks a parent comment recorded by the same
// transaction that blocked the parent and inserted its subtasks. It carries
// the number of subtasks that transaction created; the parent->child links
// themselves live in the task_relations table, so the resume path can
// identify the children even if the comment is later lost.
//
// The kanban returns a BLOCKED parent to READY once every child completes;
// without this marker the resumed parent would be re-prompted with the
// identical task and could be decomposed again forever.
const breakdownSubtasksPrefix = "[worker-breakdown] subtasks="

// breakdownMarker is the server-generated comment persisted with a worker
// breakdown. It is written by BlockTaskWithSubtasksAndComments, never by the
// generic AddComment path, so it lands atomically with the children: a failed
// marker write rolls the breakdown back instead of leaving children running
// with no record of why the parent was blocked.
func breakdownMarker(count int) models.Comment {
	return models.Comment{
		Author: models.CommentAuthorWorkerAgent,
		Body:   breakdownSubtasksPrefix + strconv.Itoa(count),
	}
}

// blockTaskWithBreakdown blocks parent and inserts drafts plus the breakdown
// marker in one transaction when the store supports it. Stores without the
// atomic variant fall back to the plain breakdown plus a separate marker
// write; the fallback is logged because losing the marker costs the roll-up.
func blockTaskWithBreakdown(
	ctx context.Context,
	store models.KanbanStore,
	parent models.Task,
	drafts []models.DraftTask,
) (*models.Task, []models.Task, error) {
	marker := breakdownMarker(len(drafts))
	marker.TaskID = parent.ID
	atomicStore, ok := store.(BlockTaskWithSubtasksAndCommentsStore)
	if !ok {
		blocked, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, drafts)
		if err != nil {
			return nil, nil, err
		}
		if err := store.AddComment(ctx, marker); err != nil {
			slog.Error("failed to record breakdown marker; parent will be re-prompted on resume",
				"task_id", parent.ID, "error", err)
		}
		return blocked, children, nil
	}
	return atomicStore.BlockTaskWithSubtasksAndComments(ctx, parent.ID, parent.UpdatedAt, drafts, []models.Comment{marker})
}

// latestBreakdownSubtaskCount returns the subtask count from the most recent
// server-generated breakdown marker on the task. Only worker-agent markers
// count: board.add_comment lets a caller choose the author, so authorship
// alone is not trustworthy. The returned count is cross-checked against the
// parent->child links (see breakdownChildrenCompleted) before the parent is
// ever completed.
func latestBreakdownSubtaskCount(comments []models.Comment) (int, bool) {
	count := 0
	var latest time.Time
	found := false
	for _, c := range comments {
		if c.Author != models.CommentAuthorWorkerAgent {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(c.Body, breakdownSubtasksPrefix)))
		if err != nil || n <= 0 || !strings.HasPrefix(c.Body, breakdownSubtasksPrefix) {
			continue
		}
		if found && c.CreatedAt.Before(latest) {
			continue
		}
		count, latest, found = n, c.CreatedAt, true
	}
	return count, found
}

// breakdownChildrenCompleted reports whether the parent's recorded child set
// is exactly the one the marker describes and every member has completed. The
// count check is what stops a forged marker naming a subset of the children:
// the relations table is written by the breakdown transaction, so it is the
// authoritative child set.
func breakdownChildrenCompleted(children []models.Task, count int) bool {
	if count <= 0 || len(children) != count {
		return false
	}
	for _, child := range children {
		if child.State != models.TaskStateCompleted {
			return false
		}
	}
	return true
}

// tryRollUpBreakdown completes a resumed parent whose breakdown subtasks have
// all completed, instead of asking the model to redo the decomposed work. It
// reports whether the task was handled; a non-nil error means the roll-up
// state could not be read or the parent could not be completed, and the
// caller must requeue rather than fall through to normal dispatch.
func (w *Worker) tryRollUpBreakdown(ctx context.Context, task models.Task) (bool, error) {
	comments, err := w.store.ListComments(ctx, task.ID)
	if err != nil {
		return false, fmt.Errorf("breakdown roll-up: list comments: %w", err)
	}
	count, marked := latestBreakdownSubtaskCount(comments)
	if !marked {
		return false, nil
	}
	children, err := w.store.ListChildTasks(ctx, task.ID)
	if err != nil {
		return false, fmt.Errorf("breakdown roll-up: list children: %w", err)
	}
	if !breakdownChildrenCompleted(children, count) {
		return false, nil
	}
	ids := make([]string, len(children))
	for i, child := range children {
		ids[i] = child.ID
	}
	summary := fmt.Sprintf("completed via %d breakdown subtasks: %s", len(ids), strings.Join(ids, ", "))
	if !w.commitSucceeded(ctx, task, sandbox.Result{Success: true, Stdout: summary}, nil) {
		// commitSucceeded already emitted the failure; resolve it explicitly
		// so the parent is retried instead of being left to reconciliation.
		return true, fmt.Errorf("breakdown roll-up: commit parent %s: unresolved", task.ID)
	}
	w.Emit(ctx, task, "TASK_BREAKDOWN_ROLLUP", summary)
	return true, nil
}
