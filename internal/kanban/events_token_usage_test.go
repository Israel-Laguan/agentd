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
	appendTokenUsageEvent := func(ts time.Time, payload, label string) {
		t.Helper()
		if err := store.AppendEvent(ctx, models.Event{
			BaseEntity: models.BaseEntity{CreatedAt: ts, UpdatedAt: ts},
			ProjectID:  task.ProjectID,
			TaskID:     sql.NullString{String: task.ID, Valid: true},
			Type:       models.EventTypeTokenUsage,
			Payload:    payload,
		}); err != nil {
			t.Fatalf("append %s: %v", label, err)
		}
	}

	now := utcNow()
	old := now.Add(-2 * time.Hour)
	appendTokenUsageEvent(old, `{"tokens":10}`, "old event")
	appendTokenUsageEvent(now, `{"tokens":5}`, "recent event")
	appendTokenUsageEvent(now.Add(time.Minute), `7`, "legacy integer event")
	appendTokenUsageEvent(now.Add(2*time.Minute), `7junk`, "malformed event")

	events, err := store.ListTokenUsageEventsSince(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListTokenUsageEventsSince: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want two valid entries", events)
	}
	if events[0].Tokens != 5 || events[1].Tokens != 7 {
		t.Fatalf("events tokens = [%d %d], want [5 7]", events[0].Tokens, events[1].Tokens)
	}
}

func TestParseTokenUsagePayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		payload string
		wantN   int
		wantOk  bool
	}{
		{`{"tokens":7}`, 7, true},
		{`7`, 7, true},
		{`  7  `, 7, true},
		{`7junk`, 0, false},
		{`junk`, 0, false},
		{`0`, 0, false},
		{`-3`, 0, false},
	}
	for _, tc := range cases {
		n, ok := parseTokenUsagePayload(tc.payload)
		if ok != tc.wantOk || n != tc.wantN {
			t.Errorf("parseTokenUsagePayload(%q) = (%d, %v), want (%d, %v)",
				tc.payload, n, ok, tc.wantN, tc.wantOk)
		}
	}
}
