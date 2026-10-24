package memory

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// T-057 guard: dream merges span many tasks, so its call stays unlabelled
// (empty TaskID) on purpose, even when the caller's ctx carries a task.
func TestDreamMergeCallStaysUnlabelledInATaskContext(t *testing.T) {
	t.Parallel()

	gw := &taskIDRecordingGateway{}
	d := &DreamAgent{Store: &fakeStore{}, Gateway: gw}
	ctx := gateway.WithTaskID(context.Background(), "task-9")

	if _, err := d.generateMergedSummary(ctx, []models.Memory{{}, {}}); err != nil {
		t.Fatalf("generateMergedSummary: %v", err)
	}
	if len(gw.taskIDs) != 1 || gw.taskIDs[0] != "" {
		t.Errorf("dream TaskIDs = %q, want one empty TaskID", gw.taskIDs)
	}
}
