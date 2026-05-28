package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

func TestAddTokenUsage_AccumulatesAndSum(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tokens",
		Tasks: []models.DraftTask{
			{Title: "a", Description: "one"},
			{Title: "b", Description: "two"},
		},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}

	if err := store.AddTokenUsage(ctx, tasks[0].ID, 10); err != nil {
		t.Fatalf("AddTokenUsage first: %v", err)
	}
	if err := store.AddTokenUsage(ctx, tasks[0].ID, 5); err != nil {
		t.Fatalf("AddTokenUsage second: %v", err)
	}
	if err := store.AddTokenUsage(ctx, tasks[1].ID, 7); err != nil {
		t.Fatalf("AddTokenUsage other task: %v", err)
	}

	got, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TokenUsage != 15 {
		t.Errorf("task[0].TokenUsage = %d, want 15", got.TokenUsage)
	}

	total, err := store.SumTokenUsage(ctx)
	if err != nil {
		t.Fatalf("SumTokenUsage: %v", err)
	}
	if total != 22 {
		t.Errorf("SumTokenUsage = %d, want 22", total)
	}
}

func TestAddTokenUsage_NoOpForNonPositive(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "tokens-noop",
		Tasks:       []models.DraftTask{{Title: "a", Description: "one"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}

	if err := store.AddTokenUsage(ctx, tasks[0].ID, 0); err != nil {
		t.Fatalf("AddTokenUsage zero: %v", err)
	}
	if err := store.AddTokenUsage(ctx, tasks[0].ID, -3); err != nil {
		t.Fatalf("AddTokenUsage negative: %v", err)
	}

	got, err := store.GetTask(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.TokenUsage != 0 {
		t.Errorf("TokenUsage = %d, want 0", got.TokenUsage)
	}
	t.Run("unknown task id returns error", func(t *testing.T) {
		err := store.AddTokenUsage(ctx, "nonexistent-task-id", 5)
		if err == nil {
			t.Fatal("AddTokenUsage with unknown taskID: want error, got nil")
		}
		if _, getErr := store.GetTask(ctx, "nonexistent-task-id"); getErr == nil {
			t.Fatal("GetTask for unknown taskID: want error, got nil")
		}
	})
}
