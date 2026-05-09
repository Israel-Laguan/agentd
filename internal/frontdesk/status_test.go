package frontdesk_test

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/frontdesk"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestSummarizeWithTasks(t *testing.T) {
	store := newSummarizerTestStore(t)
	materializeSummarizerPlan(t, store)
	summarizer := frontdesk.NewStatusSummarizer(store)

	report, err := summarizer.Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if report.Kind != "status_report" {
		t.Fatalf("Kind = %q, want status_report", report.Kind)
	}
	if report.Summary.TotalProjects != 1 {
		t.Fatalf("TotalProjects = %d, want 1", report.Summary.TotalProjects)
	}
	if report.Summary.TasksByState["READY"] != 2 {
		t.Fatalf("TasksByState[READY] = %d, want 2", report.Summary.TasksByState["READY"])
	}
	if !strings.Contains(report.Message, "2 task(s) remaining") {
		t.Fatalf("Message missing remaining count: %q", report.Message)
	}
}

func TestSummarizeEmptyBoard(t *testing.T) {
	store := newSummarizerTestStore(t)
	summarizer := frontdesk.NewStatusSummarizer(store)

	report, err := summarizer.Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if report.Kind != "status_report" {
		t.Fatalf("Kind = %q, want status_report", report.Kind)
	}
	if report.Summary.TotalProjects != 0 {
		t.Fatalf("TotalProjects = %d, want 0", report.Summary.TotalProjects)
	}
	if !strings.Contains(report.Message, "No active projects") {
		t.Fatalf("Message = %q, want empty board message", report.Message)
	}
}

func newSummarizerTestStore(_ *testing.T) *testutil.FakeKanbanStore {
	return testutil.NewFakeStore()
}

func materializeSummarizerPlan(t *testing.T, store *testutil.FakeKanbanStore) {
	t.Helper()
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "test-project",
		Description: "test",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "Task A"},
			{TempID: "b", Title: "Task B"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan() error = %v", err)
	}
}
