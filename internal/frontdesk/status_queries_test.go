package frontdesk_test

import (
	"context"
	"fmt"
	"testing"

	"agentd/internal/frontdesk"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

// taskListCounter counts the per-project full-row loads a summary performs.
type taskListCounter struct {
	*testutil.FakeKanbanStore
	listCalls int
}

func (c *taskListCounter) ListTasksByProject(ctx context.Context, projectID string) ([]models.Task, error) {
	c.listCalls++
	return c.FakeKanbanStore.ListTasksByProject(ctx, projectID)
}

// TestSummarizeDoesNotLoadEveryProjectsTasks proves the status summary counts
// states without loading each project's full task rows (T-047).
func TestSummarizeDoesNotLoadEveryProjectsTasks(t *testing.T) {
	store := &taskListCounter{FakeKanbanStore: testutil.NewFakeStore()}
	for i := 0; i < 4; i++ {
		_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
			ProjectName: fmt.Sprintf("project-%d", i),
			Tasks:       []models.DraftTask{{TempID: "a", Title: "A"}, {TempID: "b", Title: "B"}},
		})
		if err != nil {
			t.Fatalf("MaterializePlan: %v", err)
		}
	}
	report, err := frontdesk.NewStatusSummarizer(store).Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if report.Summary.TasksByState["READY"] != 8 || report.Summary.TotalProjects != 4 {
		t.Fatalf("summary = %+v, want 4 projects and 8 READY", report.Summary)
	}
	if store.listCalls != 0 {
		t.Fatalf("ListTasksByProject called %d times, want 0 (one grouped count query)", store.listCalls)
	}
}
