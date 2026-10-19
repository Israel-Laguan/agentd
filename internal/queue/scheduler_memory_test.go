//go:build integration

package queue

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/memory"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestMemoryRecallProviderUsesTitleWhenIntentMissing(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.RecordMemory(ctx, models.Memory{
		ID: "m1", Scope: "GLOBAL",
		Symptom:  sql.NullString{String: "symptom", Valid: true},
		Solution: sql.NullString{String: "fix", Valid: true},
	}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	p := memoryRecallProvider{retriever: &memory.Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: time.Second}}}
	body, err := p.Fetch(ctx, models.ScheduledTask{Title: "symptom"}, project.ID)
	if err != nil || !strings.Contains(body, "symptom") {
		t.Fatalf("Fetch body = %q err = %v", body, err)
	}
}

func TestMemoryRecallProviderFetch(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, err := store.EnsureSystemProject(ctx)
	if err != nil {
		t.Fatalf("EnsureSystemProject: %v", err)
	}
	if err := store.RecordMemory(ctx, models.Memory{
		ID: "m1", Scope: "GLOBAL",
		Symptom:  sql.NullString{String: "slow build", Valid: true},
		Solution: sql.NullString{String: "use cache", Valid: true},
	}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	providers := defaultContextProviders(store, config.LibrarianConfig{RecallTimeout: time.Second, RecallTopK: 5})
	p, ok := providers["memory_recall"].(memoryRecallProvider)
	if !ok {
		t.Fatal("memory_recall provider missing")
	}
	body, err := p.Fetch(ctx, models.ScheduledTask{
		ContextArgs: map[string]string{"intent": "build"},
		Title:       "Recall",
	}, project.ID)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(body, "LESSONS LEARNED") || !strings.Contains(body, "slow build") {
		t.Fatalf("Fetch body = %q, want lesson content", body)
	}
}

func TestMemoryRecallProviderPreferences(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	if err := store.RecordMemory(ctx, models.Memory{
		ID: "pref1", Scope: "USER_PREFERENCE",
		Solution: sql.NullString{String: "be concise", Valid: true},
	}); err != nil {
		t.Fatalf("RecordMemory: %v", err)
	}
	p := memoryRecallProvider{retriever: &memory.Retriever{Store: store, Cfg: config.LibrarianConfig{RecallTimeout: time.Second}}}
	body, err := p.Fetch(ctx, models.ScheduledTask{ContextArgs: map[string]string{"user_id": "u1", "intent": "concise"}}, "p1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.Contains(body, "USER PREFERENCES") || !strings.Contains(body, "be concise") {
		t.Fatalf("Fetch body = %q, want preferences", body)
	}
}
