package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestListParentTasks_ReturnsBlockingParent(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "parents",
		Tasks:       []models.DraftTask{{Title: "root", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	parent := tasks[0]

	_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: "child", Description: "step", Assignee: models.TaskAssigneeSystem,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("children = %d, want 1", len(children))
	}

	parents, err := store.ListParentTasks(ctx, children[0].ID)
	if err != nil {
		t.Fatalf("ListParentTasks: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != parent.ID {
		t.Fatalf("parents = %+v, want [%s]", parents, parent.ID)
	}
}
