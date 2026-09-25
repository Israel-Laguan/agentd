package frontdesk_test

import (
	"context"
	"testing"

	"agentd/internal/frontdesk"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestSummarizeWithOptions_ExcludesHealingTasks(t *testing.T) {
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "status-heal-test",
		Tasks: []models.DraftTask{
			{Title: "normal-work", Description: "work"},
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	// Block with a HITL subtask.
	_, _, err = store.BlockTaskWithSubtasks(context.Background(), tasks[0].ID, tasks[0].UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualReview + " AI unavailable",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}

	sum := frontdesk.NewStatusSummarizer(store)

	// Include healing: should count all.
	inclReport, err := sum.SummarizeWithOptions(context.Background(), frontdesk.SummarizeOptions{
		IncludeHealing: true,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("SummarizeWithOptions(include): %v", err)
	}
	inclTotal := 0
	for _, c := range inclReport.Summary.TasksByState {
		inclTotal += c
	}

	// Exclude healing: should count fewer.
	exclReport, err := sum.SummarizeWithOptions(context.Background(), frontdesk.SummarizeOptions{
		IncludeHealing: false,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("SummarizeWithOptions(exclude): %v", err)
	}
	exclTotal := 0
	for _, c := range exclReport.Summary.TasksByState {
		exclTotal += c
	}

	if exclTotal >= inclTotal {
		t.Fatalf("excluding healing should reduce count: incl=%d excl=%d", inclTotal, exclTotal)
	}
}

func TestSummarizeWithOptions_ExcludesSystemProject(t *testing.T) {
	store := testutil.NewFakeStore()
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "user-project",
		Tasks:       []models.DraftTask{{Title: "user-task"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	sysProj, err := store.EnsureSystemProject(context.Background())
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	_, _, err = store.EnsureProjectTask(context.Background(), sysProj.ID, models.DraftTask{
		Title:    "System alert",
		Assignee: models.TaskAssigneeHuman,
	})
	if err != nil {
		t.Fatalf("EnsureProjectTask: %v", err)
	}

	sum := frontdesk.NewStatusSummarizer(store)

	inclReport, err := sum.SummarizeWithOptions(context.Background(), frontdesk.SummarizeOptions{
		IncludeHealing: true,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("SummarizeWithOptions(include): %v", err)
	}
	if inclReport.Summary.TotalProjects != 2 {
		t.Fatalf("expected 2 projects with include_system, got %d", inclReport.Summary.TotalProjects)
	}

	exclReport, err := sum.SummarizeWithOptions(context.Background(), frontdesk.SummarizeOptions{
		IncludeHealing: true,
		IncludeSystem:  false,
	})
	if err != nil {
		t.Fatalf("SummarizeWithOptions(exclude): %v", err)
	}
	if exclReport.Summary.TotalProjects != 1 {
		t.Fatalf("expected 1 project without _system, got %d", exclReport.Summary.TotalProjects)
	}
}

func TestSummarizeIncludesStructuredAttention(t *testing.T) {
	store := testutil.NewFakeStore()
	project, tasks, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "attention",
		Tasks:       []models.DraftTask{{Title: "Collect hardware details"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	_, children, err := store.BlockTaskWithSubtasks(context.Background(), tasks[0].ID, tasks[0].UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualAction + " privileged command",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	report, err := frontdesk.NewStatusSummarizer(store).SummarizeWithOptions(context.Background(), frontdesk.SummarizeOptions{
		IncludeHealing: true, IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	var child *frontdesk.AttentionItem
	for i := range report.Attention {
		if report.Attention[i].TaskID == children[0].ID {
			child = &report.Attention[i]
			break
		}
	}
	if child == nil {
		t.Fatalf("attention = %#v, want child %s", report.Attention, children[0].ID)
	}
	if child.ProjectID != project.ID || child.ProjectName != project.Name {
		t.Fatalf("attention project = %+v", child)
	}
	if child.RequiredAction == "" || child.Explanation == "" {
		t.Fatalf("attention action/explanation missing: %+v", child)
	}
}

func TestSummarizeWithoutAttentionKeepsEmptyList(t *testing.T) {
	store := testutil.NewFakeStore()
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "quiet",
		Tasks:       []models.DraftTask{{Title: "Ready work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	report, err := frontdesk.NewStatusSummarizer(store).Summarize(context.Background())
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if len(report.Attention) != 0 {
		t.Fatalf("attention = %#v, want empty", report.Attention)
	}
}

func TestSummarize_BackwardsCompatible(t *testing.T) {
	store := testutil.NewFakeStore()
	_, _, err := store.MaterializePlan(context.Background(), models.DraftPlan{
		ProjectName: "compat-test",
		Tasks:       []models.DraftTask{{Title: "compat-task"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	sum := frontdesk.NewStatusSummarizer(store)
	report, err := sum.Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize: %v", err)
	}
	if report.Kind != "status_report" {
		t.Fatalf("Kind = %q, want status_report", report.Kind)
	}
	if report.Summary.TotalProjects != 1 {
		t.Fatalf("TotalProjects = %d, want 1", report.Summary.TotalProjects)
	}
}
