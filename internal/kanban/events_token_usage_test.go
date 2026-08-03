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
		payload    string
		wantN      int
		wantCached int
		wantWrite  int
		wantOk     bool
	}{
		{`{"tokens":7}`, 7, 0, 0, true},
		{`{"tokens":7,"cached_tokens":3,"cache_write_tokens":2}`, 7, 3, 2, true},
		{`7`, 7, 0, 0, true},
		{`  7  `, 7, 0, 0, true},
		{`7junk`, 0, 0, 0, false},
		{`junk`, 0, 0, 0, false},
		{`0`, 0, 0, 0, false},
		{`-3`, 0, 0, 0, false},
		// Negative cache counters must be rejected.
		{`{"tokens":7,"cached_tokens":-1,"cache_write_tokens":2}`, 0, 0, 0, false},
		{`{"tokens":7,"cached_tokens":3,"cache_write_tokens":-1}`, 0, 0, 0, false},
		{`{"tokens":7,"cached_tokens":-5,"cache_write_tokens":-2}`, 0, 0, 0, false},
	}
	for _, tc := range cases {
		p, ok := parseTokenUsagePayload(tc.payload)
		if ok != tc.wantOk || p.Tokens != tc.wantN || p.CachedTokens != tc.wantCached || p.CacheWriteTokens != tc.wantWrite {
			t.Errorf("parseTokenUsagePayload(%q) = (%+v, %v), want tokens=%d cached=%d write=%d ok=%v",
				tc.payload, p, ok, tc.wantN, tc.wantCached, tc.wantWrite, tc.wantOk)
		}
	}
}

// TestListTokenUsageEventsSince_CacheFields verifies that prompt-cache fields
// stored in TOKEN_USAGE event payloads are surfaced through the parsed events
// (M15: cache observability).
func TestListTokenUsageEventsSince_CacheFields(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "token-events-cache",
		Tasks:       []models.DraftTask{{Title: "a", Description: "one"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	now := utcNow()
	appendEvent := func(ts time.Time, payload, label string) {
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
	appendEvent(now, `{"tokens":42,"cached_tokens":30,"cache_write_tokens":12}`, "openai cached")
	appendEvent(now.Add(time.Minute), `{"tokens":8,"cached_tokens":5,"cache_write_tokens":3}`, "deepseek cached")

	events, err := store.ListTokenUsageEventsSince(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListTokenUsageEventsSince: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want two entries", events)
	}
	if events[0].Tokens != 42 || events[0].CachedTokens != 30 || events[0].CacheWriteTokens != 12 {
		t.Errorf("events[0] = %+v, want tokens=42 cached=30 write=12", events[0])
	}
	if events[1].Tokens != 8 || events[1].CachedTokens != 5 || events[1].CacheWriteTokens != 3 {
		t.Errorf("events[1] = %+v, want tokens=8 cached=5 write=3", events[1])
	}
}
