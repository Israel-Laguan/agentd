package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

func TestProcess_AgenticRequireReview_RejectionMarkedOnlyAfterDraft(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupAgenticReviewProfile(t, store, ctx)

	const rejectionComment = "Please add error handling"
	const revisedDraft = "revised draft with error handling"
	gw := &sequenceGateway{
		responses: []gateway.AIResponse{
			{
				Content: "I'll check the workspace first.",
				ToolCalls: []gateway.ToolCall{{
					ID: "call_reject_1", Type: "function",
					Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command": "pwd"}`},
				}},
			},
			{Content: revisedDraft},
		},
	}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"pwd": {Success: true, ExitCode: 0, Stdout: "/tmp\n"},
	}}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})
	task := materializeLegacyReviewTask(t, store, ctx, "agentic-review-reject", "task-agentic-review-reject")
	w.createReviewHandoff(ctx, task, "first draft output")
	failReviewWithComment(t, store, ctx, task.ID, rejectionComment)
	review := findReviewSubtask(t, store, ctx, task.ID)
	queued := requeueParentTask(t, store, ctx, task.ID)

	w.Process(ctx, queued)

	if gw.callCount != 2 {
		t.Fatalf("gateway calls = %d, want 2 (tool turn + final draft)", gw.callCount)
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if !isReviewRejectionConsumed(comments, review.ID) {
		t.Fatal("expected review-rejection-used marker after revised draft persisted")
	}
	draft, ok := findLatestDraftReview(comments)
	if !ok || !strings.Contains(draft, revisedDraft) {
		t.Fatalf("draft = %q, want revised draft containing %q", draft, revisedDraft)
	}
	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	if parent.State != models.TaskStateBlocked {
		t.Fatalf("parent state = %s, want BLOCKED", parent.State)
	}
}
