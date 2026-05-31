package session

import (
	"context"
	"testing"

	"agentd/internal/gateway"
)

func TestMemoryCheckpointStore_CapsPerSession(t *testing.T) {
	t.Parallel()
	store := NewMemoryCheckpointStore()
	ctx := context.Background()
	sessionID := "sess-cap"

	var firstID, lastID string
	for i := 0; i < maxCheckpointsPerSession+1; i++ {
		id, err := store.Create(ctx, sessionID, []gateway.PromptMessage{
			{Role: "user", Content: "msg"},
		})
		if err != nil {
			t.Fatalf("Create(%d): %v", i, err)
		}
		if i == 0 {
			firstID = id
		}
		lastID = id
	}

	if _, err := store.Get(ctx, firstID); err == nil {
		t.Fatalf("expected oldest checkpoint %q to be evicted", firstID)
	}
	if _, err := store.Get(ctx, lastID); err != nil {
		t.Fatalf("expected newest checkpoint %q retrievable: %v", lastID, err)
	}
}

func TestSessionCheckpointer_CheckpointDeepCopy(t *testing.T) {
	t.Parallel()
	cp := NewSessionCheckpointer("task-1")
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "anchor"},
	}
	if err := cp.Checkpoint(prePlanCheckpointLabel, messages); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	messages = append(messages, gateway.PromptMessage{Role: "assistant", Content: "after"})
	messages[0].Content = "mutated"

	restored := []gateway.PromptMessage{{Role: "noise"}}
	if err := cp.BranchFrom(prePlanCheckpointLabel, &restored); err != nil {
		t.Fatalf("BranchFrom: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("len(restored) = %d, want 2", len(restored))
	}
	if restored[0].Content != "sys" {
		t.Fatalf("restored system = %q, want sys", restored[0].Content)
	}
	if restored[1].Content != "anchor" {
		t.Fatalf("restored user = %q, want anchor", restored[1].Content)
	}
}

func TestSessionCheckpointer_BranchFromDiscardsLaterTurns(t *testing.T) {
	t.Parallel()
	cp := NewSessionCheckpointer("task-2")
	if err := cp.Checkpoint(prePlanCheckpointLabel, []gateway.PromptMessage{
		{Role: "user", Content: "only"},
	}); err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	live := []gateway.PromptMessage{
		{Role: "user", Content: "only"},
		{Role: "assistant", Content: "bad"},
		{Role: "tool", Content: "err"},
	}
	if err := cp.BranchFrom(prePlanCheckpointLabel, &live); err != nil {
		t.Fatalf("BranchFrom: %v", err)
	}
	if len(live) != 1 || live[0].Content != "only" {
		t.Fatalf("live after restore = %+v, want single user turn", live)
	}
}

func TestSessionCheckpointer_BranchFromMissingLabel(t *testing.T) {
	t.Parallel()
	cp := NewSessionCheckpointer("task-3")
	var live []gateway.PromptMessage
	err := cp.BranchFrom("missing", &live)
	if err == nil {
		t.Fatal("expected error for missing label")
	}
	want := `checkpoint label "missing" not found for session "task-3"`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func TestSessionCheckpointer_List(t *testing.T) {
	t.Parallel()
	cp := NewSessionCheckpointer("task-4")
	if err := cp.Checkpoint("beta", nil); err != nil {
		t.Fatalf("Checkpoint(beta): %v", err)
	}
	if err := cp.Checkpoint("alpha", nil); err != nil {
		t.Fatalf("Checkpoint(alpha): %v", err)
	}
	labels := cp.List()
	if len(labels) != 2 || labels[0] != "alpha" || labels[1] != "beta" {
		t.Fatalf("List() = %v, want [alpha beta]", labels)
	}
}
