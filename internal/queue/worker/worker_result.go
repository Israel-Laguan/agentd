package worker

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	"agentd/internal/models"
)

// updateTaskResult is the single write path for a terminal task result: it
// persists through the store and then fans the RESULT event out to live
// subscribers. Every completion path goes through here rather than calling
// store.UpdateTaskResult directly, because that store call is what appends the
// RESULT event — a path that skipped the broadcast left SSE subscribers (the
// web UI's live board among them) showing a task as still running until the
// next poll or reconnect.
func (w *Worker) updateTaskResult(ctx context.Context, task models.Task, result models.TaskResult) (*models.Task, error) {
	if result.Success && !w.checkGoldenCriteria(ctx, task) {
		return nil, fmt.Errorf("golden checks rejected task completion")
	}
	updated, err := w.store.UpdateTaskResult(ctx, task.ID, task.UpdatedAt, result)
	if err != nil {
		return updated, err
	}
	w.broadcastResult(ctx, task, result.Payload)
	return updated, nil
}

// broadcastResult fans the task's RESULT event out to live subscribers.
//
// The event itself is already durable: Store.UpdateTaskResult appends it via
// kanban/db.AppendTaskResultEvent inside the same transaction that flips the
// task to COMPLETED, so it must not be re-persisted here. It was, however,
// never published to the event bus, so SSE subscribers (the web UI's live
// board among them) saw a task start producing output and then went silent —
// completion only became visible on the next poll or reconnect. Broadcasting
// the already-written event closes that gap.
func (w *Worker) broadcastResult(ctx context.Context, task models.Task, payload string) {
	// The store writes no RESULT row for a blank payload (B-097); announcing
	// one would show subscribers an event the events API never returns.
	if strings.TrimSpace(payload) == "" {
		return
	}
	broadcaster, ok := w.sink.(models.EventBroadcaster)
	if !ok {
		return
	}
	if err := broadcaster.Broadcast(ctx, models.Event{
		ProjectID: task.ProjectID,
		TaskID:    sql.NullString{String: task.ID, Valid: true},
		Type:      models.EventTypeResult,
		Payload:   payload,
	}); err != nil {
		slog.Warn("failed to broadcast task result to live subscribers", "task_id", task.ID, "error", err)
	}
}
