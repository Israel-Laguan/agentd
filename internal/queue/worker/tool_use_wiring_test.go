package worker

import (
	"context"
	"testing"

	agenthooks "agentd/internal/agent/hooks"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

type toolCallStore struct {
	*testutil.FakeKanbanStore
	calls []models.ToolCall
}

func (s *toolCallStore) InsertToolCall(_ context.Context, c models.ToolCall) error {
	s.calls = append(s.calls, c)
	return nil
}

func TestWorkerRecordsToolCallsWhenTheStoreKeepsThem(t *testing.T) {
	store := &toolCallStore{FakeKanbanStore: testutil.NewFakeStore()}
	w := NewWorker(store, nil, nil, nil, nil, WorkerOptions{})
	w.hooks.RunPost(agenthooks.HookContext{ToolName: "read", Args: `{"path":"a.go"}`, SessionID: "t1", CallID: "c1"}, "abc")
	if len(store.calls) != 1 || store.calls[0].Path != "a.go" || store.calls[0].ResultBytes != 3 {
		t.Fatalf("recorded = %+v, want one read of a.go with 3 bytes", store.calls)
	}
}
