package testutil

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestListParentTasks_ReturnsDependencyParent(t *testing.T) {
	t.Parallel()
	store := NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "deps",
		Tasks: []models.DraftTask{
			{TempID: "a", Title: "root", Description: "first"},
			{TempID: "b", Title: "follow", Description: "second", DependsOn: []string{"a"}},
		},
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}
	var child, parent models.Task
	for _, task := range tasks {
		if task.Title == "follow" {
			child = task
		} else {
			parent = task
		}
	}

	parents, err := store.ListParentTasks(ctx, child.ID)
	if err != nil {
		t.Fatalf("ListParentTasks: %v", err)
	}
	if len(parents) != 1 || parents[0].ID != parent.ID {
		t.Fatalf("parents = %+v, want [%s]", parents, parent.ID)
	}
}
