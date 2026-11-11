package worker

import (
	"context"
	"log/slog"

	"agentd/internal/models"
)

// ProcessBatch dispatches each task through its own execution route.
//
// It used to run one batched LLM call for the tasks of a batch, with a per-slot
// fallback. That went with the builtin exec path in T-070: a task that reaches
// exec.turn now runs on its own external agent, and one agent session cannot
// serve two tasks. The batch text gateway stays for its unit tests, which pin
// the request and response shape the L1 path uses.
func (w *Worker) ProcessBatch(ctx context.Context, tasks []models.Task) {
	if len(tasks) > 1 {
		ids := make([]string, len(tasks))
		for i, task := range tasks {
			ids[i] = task.ID
		}
		slog.DebugContext(ctx, "worker: processing batch", "batch_size", len(tasks), "task_ids", ids)
	}
	for _, task := range tasks {
		w.Process(ctx, task)
	}
}
