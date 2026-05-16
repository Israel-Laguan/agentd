package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestPrependReviewRejectionFeedback_InjectsOnce(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "review-reject",
		Tasks:       []models.DraftTask{{Title: "parent", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	parent := tasks[0]

	_, children, err := store.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title:    reviewSubtaskTitlePrefix + " draft output pending approval",
		Assignee: models.TaskAssigneeHuman,
	}})
	if err != nil {
		t.Fatalf("block: %v", err)
	}
	review := children[0]
	if err := store.AddComment(ctx, models.Comment{
		TaskID: review.ID,
		Author: models.CommentAuthorUser,
		Body:   "Please add error handling",
	}); err != nil {
		t.Fatalf("add rejection comment: %v", err)
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateFailed); err != nil {
		t.Fatalf("fail review subtask: %v", err)
	}

	w := &Worker{store: store}
	base := []gateway.PromptMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "task"}}

	first := w.prependReviewRejectionFeedback(ctx, parent, base)
	if len(first) != len(base)+1 {
		t.Fatalf("messages = %d, want %d", len(first), len(base)+1)
	}
	if !strings.Contains(first[len(first)-1].Content, "Please add error handling") {
		t.Fatalf("feedback = %q, want rejection reason", first[len(first)-1].Content)
	}

	second := w.prependReviewRejectionFeedback(ctx, parent, base)
	if len(second) != len(base) {
		t.Fatalf("second call messages = %d, want %d (rejection already consumed)", len(second), len(base))
	}
}

func TestCreatePromptHandoff_RecordsLegacyExpiry(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	sink := &mockEventSink{}
	w := &Worker{store: store, sink: sink}
	ctx := context.Background()

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "prompt-expiry",
		Tasks:       []models.DraftTask{{Title: "task-prompt", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	before := time.Now()
	w.createPromptHandoff(ctx, task, "interactive prompt detected")
	after := time.Now()

	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	var expiry time.Time
	var found bool
	for _, c := range comments {
		if !strings.HasPrefix(c.Body, hitlExpiresAtPrefix) {
			continue
		}
		raw := strings.TrimPrefix(c.Body, hitlExpiresAtPrefix)
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		expiry = parsed
		found = true
		break
	}
	if !found {
		t.Fatal("expected legacy handoff expiry comment")
	}
	expiry = expiry.UTC().Truncate(time.Second)
	wantMin := before.Add(LegacyHandoffTimeout).UTC().Truncate(time.Second)
	wantMax := after.Add(LegacyHandoffTimeout).UTC().Truncate(time.Second)
	if expiry.Before(wantMin) || expiry.After(wantMax) {
		t.Fatalf("expiry = %s, want in [%s, %s]", expiry, wantMin, wantMax)
	}
}

func TestProcess_LegacyRequireReview_FinalizesApprovedReview(t *testing.T) {
	t.Parallel()
	store := testutil.NewFakeStore()
	ctx := context.Background()

	profile, err := store.GetAgentProfile(ctx, "default")
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	profile.RequireReview = true
	profile.AgenticMode = false
	if err := store.UpsertAgentProfile(ctx, *profile); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}

	gw := &routingTestGateway{}
	sb := &routingTestSandbox{}
	w := NewWorker(store, gw, sb, nil, nil, WorkerOptions{MaxToolIterations: 5})

	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "legacy-review-finalize",
		Tasks:       []models.DraftTask{{Title: "task-review-finalize", Description: "work"}},
	})
	if err != nil {
		t.Fatalf("materialize plan: %v", err)
	}
	task := tasks[0]

	w.createReviewHandoff(ctx, task, "approved draft output")

	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("list children: %v", err)
	}
	var review *models.Task
	for i := range children {
		if strings.HasPrefix(children[i].Title, reviewSubtaskTitlePrefix) {
			review = &children[i]
			break
		}
	}
	if review == nil {
		t.Fatal("expected review subtask")
	}
	if _, err := store.UpdateTaskState(ctx, review.ID, review.UpdatedAt, models.TaskStateCompleted); err != nil {
		t.Fatalf("complete review: %v", err)
	}

	parent, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get parent: %v", err)
	}
	queued, err := store.UpdateTaskState(ctx, parent.ID, parent.UpdatedAt, models.TaskStateQueued)
	if err != nil {
		t.Fatalf("requeue parent: %v", err)
	}

	w.Process(ctx, *queued)

	final, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("get final parent: %v", err)
	}
	if final.State != models.TaskStateCompleted {
		t.Fatalf("parent state = %s, want COMPLETED", final.State)
	}
	if sb.execCount != 0 {
		t.Fatalf("sandbox executions = %d, want 0 (approved review should not re-run command)", sb.execCount)
	}
	if len(gw.requests) != 0 {
		t.Fatalf("gateway requests = %d, want 0", len(gw.requests))
	}
}
