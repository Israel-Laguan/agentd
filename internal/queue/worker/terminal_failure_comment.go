package worker

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// postFailureComment leaves a human-readable note on a task that has reached a
// terminal failure, so an operator reading the board sees why without having
// to dig through the raw event list. Failure to post is reported as an ERROR
// event and never changes the task's state.
func (w *Worker) postFailureComment(ctx context.Context, task models.Task, summary, lastError string) {
	body := fmt.Sprintf(
		"%s\n\nLast error: %s\n\nNext step: fix the cause (for example the task description or the project workspace), then retry with POST /api/v1/tasks/%s/retry.",
		summary, truncate(lastError, 1000), task.ID,
	)
	err := w.store.AddComment(ctx, models.Comment{
		TaskID: task.ID,
		Author: models.CommentAuthorWorkerAgent,
		Body:   body,
	})
	if err != nil {
		w.Emit(ctx, task, "ERROR", "post failure comment: "+err.Error())
	}
}
