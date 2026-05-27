package kanban

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
)

func TestListTasksExcludeHealingPreservesPagination(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "healing-page",
		Tasks: []models.DraftTask{
			{Title: "one", Description: "one"},
			{Title: "two", Description: "two"},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	parent := tasks[0]
	if _, _, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualReview + " outage",
		Assignee: models.TaskAssigneeHuman,
	}}); err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}

	pid := proj.ID
	withHealing, err := store.ListTasks(ctx, models.TaskFilter{ProjectID: &pid, IncludeHealing: true})
	if err != nil {
		t.Fatalf("ListTasks with healing: %v", err)
	}
	if withHealing.Total < 3 {
		t.Fatalf("with healing total = %d, want at least 3", withHealing.Total)
	}

	page, err := store.ListTasks(ctx, models.TaskFilter{
		ProjectID:      &pid,
		IncludeHealing: false,
		Pagination:     models.PaginationParams{Limit: 1, Offset: 0},
	})
	if err != nil {
		t.Fatalf("ListTasks exclude healing: %v", err)
	}
	wantTotal := withHealing.Total - 1
	if page.Total != wantTotal {
		t.Fatalf("total = %d, want %d (all tasks minus one healing handoff)", page.Total, wantTotal)
	}
	if !page.HasNext {
		t.Fatalf("HasNext = false, want true when more non-healing tasks exist (total=%d)", page.Total)
	}
	for _, task := range page.Data {
		if models.IsSelfHealingHandoffTask(task) {
			t.Fatalf("page included healing task %q", task.Title)
		}
	}
}

func TestListTasksIncludeHealingListsHandoff(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "healing-visible",
		Tasks:       []models.DraftTask{{Title: "work", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	parent := tasks[0]
	if _, _, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    models.HITLSubtaskTitleManualReview + " AI unavailable",
		Assignee: models.TaskAssigneeHuman,
	}}); err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}

	pid := proj.ID
	page, err := store.ListTasks(ctx, models.TaskFilter{ProjectID: &pid, IncludeHealing: true})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	var found bool
	for _, task := range page.Data {
		if strings.HasPrefix(task.Title, models.HITLSubtaskTitleManualReview) {
			found = true
		}
	}
	if !found {
		t.Fatal("include_healing list should return self-healing handoff subtask")
	}
}

func TestListTasksExcludeHealing_DoesNotFilterWrongCaseTitle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	proj, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "healing-case",
		Tasks:       []models.DraftTask{{Title: "work", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	parent := tasks[0]
	decoyTitle := "manual review required: decoy"
	if _, _, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    decoyTitle,
		Assignee: models.TaskAssigneeHuman,
	}}); err != nil {
		t.Fatalf("BlockTaskWithSubtasks: %v", err)
	}
	if models.IsSelfHealingHandoffTask(models.Task{Assignee: models.TaskAssigneeHuman, Title: decoyTitle}) {
		t.Fatal("decoy title must not match IsSelfHealingHandoffTask (case-sensitive prefix)")
	}

	pid := proj.ID
	page, err := store.ListTasks(ctx, models.TaskFilter{ProjectID: &pid, IncludeHealing: false})
	if err != nil {
		t.Fatalf("ListTasks exclude healing: %v", err)
	}
	var found bool
	for _, task := range page.Data {
		if task.Title == decoyTitle {
			found = true
		}
	}
	if !found {
		t.Fatal("wrong-case HUMAN title must remain visible when include_healing=false (SQL must match Go prefix semantics)")
	}
}
