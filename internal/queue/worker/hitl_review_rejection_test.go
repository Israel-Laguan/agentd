package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestProcess_AgenticRequireReview_RejectionMarkedOnlyAfterDraft(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()
	setupAgenticReviewProfile(t, store, ctx)

	const rejectionComment = "Please add error handling"
	const revisedDraft = "revised draft with error handling"
	rt, _, log := acpRuntime(t)
	w := NewWorker(store, nil, &mockAgenticSandbox{}, nil, nil, WorkerOptions{Routes: acpRoutes(), ACP: rt})
	task := materializeLegacyReviewTask(t, store, ctx, "agentic-review-reject", "task-agentic-review-reject")
	updated, err := store.UpdateTaskDescription(ctx, task.ID, task.UpdatedAt, task.Description+"\n"+fakeagent.Script(report(map[string]any{"summary": revisedDraft})))
	if err != nil {
		t.Fatal(err)
	}
	task = *updated
	w.createReviewHandoff(ctx, task, "first draft output")
	failReviewWithComment(t, store, ctx, task.ID, rejectionComment)
	review := findReviewSubtask(t, store, ctx, task.ID)
	queued := requeueParentTask(t, store, ctx, task.ID)

	w.Process(ctx, queued)

	if starts, _ := log.runs(); len(starts) != 1 {
		t.Fatalf("ACP sessions = %d, want 1", len(starts))
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
