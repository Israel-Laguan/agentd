package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// B-039 on the agentic path: a text-only finish is not COMPLETED while a
// success-criteria file it names is missing. The task goes to a human, as an
// unverified finish does.
func TestAgenticFinishWithUnmetSuccessCriteriaHandsTaskToHuman(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "Done. The report is written."},
	}}
	draft := models.DraftTask{
		Title:           "Write the report",
		Description:     testutil.AgenticTestTaskDescription(),
		SuccessCriteria: []string{"report.md is written"},
	}
	w, _, task := runAgenticDraft(t, gw, &scriptedSandbox{}, draft, nil)

	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN: report.md does not exist", got.State)
	}
}

// The same finish completes once the criteria file exists and is not empty.
func TestAgenticFinishWithMetSuccessCriteriaCompletes(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "Done. The report is written."},
	}}
	draft := models.DraftTask{
		Title:           "Write the report",
		Description:     testutil.AgenticTestTaskDescription(),
		SuccessCriteria: []string{"report.md is written"},
	}
	prep := func(workspace string) {
		if err := os.WriteFile(filepath.Join(workspace, "report.md"), []byte("# Report\n"), 0o644); err != nil {
			t.Fatalf("seed report.md: %v", err)
		}
	}
	w, _, task := runAgenticDraft(t, gw, &scriptedSandbox{}, draft, prep)

	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED: report.md exists", got.State)
	}
}
