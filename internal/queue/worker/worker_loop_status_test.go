package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func unknownLoopResult() LoopResult {
	return LoopResult{
		Status: 99,
		Meta: LoopMeta{
			LastError: "boom",
			ToolName:  "bash",
		},
	}
}

func TestHandleLoopResult_UnknownStatus_FallsThroughFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "p",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "d"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	sink := &mockEventSink{}
	w := NewWorker(store, nil, nil, nil, sink, WorkerOptions{MaxRetries: 1})
	w.handleLoopResult(ctx, task, unknownLoopResult())

	updated, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if updated.State != models.TaskStateFailedRequiresHuman {
		t.Errorf("state = %v, want %v", updated.State, models.TaskStateFailedRequiresHuman)
	}
	var found bool
	for _, event := range sink.events {
		if string(event.Type) == "LOOP_UNKNOWN_STATUS" {
			found = true
			if !strings.Contains(event.Payload, "status=unknown_loop_status(99)") {
				t.Errorf("payload missing status: %q", event.Payload)
			}
			if !strings.Contains(event.Payload, "last_error=boom") {
				t.Errorf("payload missing last_error: %q", event.Payload)
			}
			if !strings.Contains(event.Payload, "tool=bash") {
				t.Errorf("payload missing tool: %q", event.Payload)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected LOOP_UNKNOWN_STATUS event")
	}
}
