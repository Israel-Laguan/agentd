package kanban

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"agentd/internal/models"
)

func TestListTokenUsageEventsSince(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "token-events",
		Tasks:       []models.DraftTask{{Title: "a", Description: "one"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]

	now := utcNow()
	old := now.Add(-2 * time.Hour)
	if err := store.AppendEvent(ctx, models.Event{
		BaseEntity: models.BaseEntity{CreatedAt: old, UpdatedAt: old},
		ProjectID:  task.ProjectID,
		TaskID:     sql.NullString{String: task.ID, Valid: true},
		Type:       models.EventTypeTokenUsage,
		Payload:    `{"tokens":10}`,
	}); err != nil {
		t.Fatalf("append old event: %v", err)
	}
	if err := store.AppendEvent(ctx, models.Event{
		BaseEntity: models.BaseEntity{CreatedAt: now, UpdatedAt: now},
		ProjectID:  task.ProjectID,
		TaskID:     sql.NullString{String: task.ID, Valid: true},
		Type:       models.EventTypeTokenUsage,
		Payload:    `{"tokens":5}`,
	}); err != nil {
		t.Fatalf("append recent event: %v", err)
	}

	events, err := store.ListTokenUsageEventsSince(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListTokenUsageEventsSince: %v", err)
	}
	if len(events) != 1 || events[0].Tokens != 5 {
		t.Fatalf("events = %+v, want one entry with 5 tokens", events)
	}
}
