package worker

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
