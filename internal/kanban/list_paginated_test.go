package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func testPaginatedProjects(t *testing.T, store *Store, ctx context.Context) {
	t.Helper()
	page1, err := store.ListProjectsPage(ctx, models.PaginationParams{Limit: 1, Offset: 0})
	if err != nil {
		t.Fatalf("ListProjectsPage: %v", err)
	}
	if len(page1.Data) != 1 || page1.Total < 2 || !page1.HasNext {
		t.Fatalf("page1 = %+v", page1)
	}
	page2, err := store.ListProjectsPage(ctx, models.PaginationParams{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("ListProjectsPage page2: %v", err)
	}
	if len(page2.Data) != 1 {
		t.Fatalf("page2 = %+v", page2)
	}
	if _, err := store.ListProjectsPage(ctx, models.PaginationParams{Limit: 1, SortBy: "not_a_column", Order: "ASC"}); err != nil {
		t.Fatalf("invalid sort: %v", err)
	}
}

func seedPaginatedProjects(t *testing.T, store *Store, ctx context.Context) models.Project {
	t.Helper()
	if _, _, err := store.MaterializePlan(ctx, samplePlan()); err != nil {
		t.Fatalf("MaterializePlan 1: %v", err)
	}
	plan2 := samplePlan()
	plan2.ProjectName = "second project"
	proj2, _, err := store.MaterializePlan(ctx, plan2)
	if err != nil {
		t.Fatalf("MaterializePlan 2: %v", err)
	}
	return *proj2
}

func TestPaginatedProjectsAndTasks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	proj2 := seedPaginatedProjects(t, store, ctx)

	t.Run("projects", func(t *testing.T) {
		testPaginatedProjects(t, store, ctx)
	})

	t.Run("tasks", func(t *testing.T) {
		all, err := store.ListTasks(ctx, models.TaskFilter{})
		if err != nil {
			t.Fatalf("ListTasks: %v", err)
		}
		if all.Total < 2 {
			t.Fatalf("total tasks = %d", all.Total)
		}

		pid := proj2.ID
		filtered, err := store.ListTasks(ctx, models.TaskFilter{
			ProjectID: &pid,
			States:    []models.TaskState{models.TaskStateReady},
			Pagination: models.PaginationParams{
				Limit: 10,
			},
		})
		if err != nil {
			t.Fatalf("ListTasks filtered: %v", err)
		}
		if len(filtered.Data) == 0 {
			t.Fatal("expected at least one filtered task")
		}
		for _, task := range filtered.Data {
			if task.ProjectID != pid {
				t.Fatalf("task project = %s, want %s", task.ProjectID, pid)
			}
			if task.State != models.TaskStateReady {
				t.Fatalf("task state = %s", task.State)
			}
		}
	})
}
