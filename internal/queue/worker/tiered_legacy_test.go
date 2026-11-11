package worker

import (
	"context"
	"os"
	"testing"

	"agentd/internal/models"
)

// runLegacyTieredTest keeps the tiered pipeline's own tests independent of
// dispatch until the pipeline moves with the harness in T-071.
func runLegacyTieredTest(t *testing.T, ctx context.Context, w *Worker, task models.Task) {
	t.Helper()
	project, profile, err := w.loadContext(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if w.isTieredStep(*running) {
		if !w.dispatchTieredStep(ctx, *running, *project, *profile) {
			t.Fatal("tiered step was not handled")
		}
		return
	}
	if !w.tryTieredOrigin(ctx, *running, nil, false, nil) {
		t.Fatal("tiered origin was not handled")
	}
}
