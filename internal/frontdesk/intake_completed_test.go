package frontdesk

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

// driveToState moves a materialized task to COMPLETED or FAILED through the
// realistic RUNNING ladder so CompletedAt is set like production.
func driveToState(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, task models.Task, success bool) models.Task {
	t.Helper()
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 4242)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	done, err := store.UpdateTaskResult(ctx, task.ID, running.UpdatedAt, models.TaskResult{Success: success})
	if err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	return *done
}

func commentAndListRef(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, taskID, body string) models.CommentRef {
	t.Helper()
	if err := store.AddComment(ctx, models.Comment{
		TaskID: taskID,
		Author: models.CommentAuthorUser,
		Body:   body,
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	refs, err := store.ListUnprocessedHumanComments(ctx)
	if err != nil {
		t.Fatalf("ListUnprocessedHumanComments: %v", err)
	}
	if len(refs) != 1 {
		t.Fatalf("unprocessed refs = %d, want 1", len(refs))
	}
	return refs[0]
}

// A human comment on a finished task must spawn follow-ups without
// re-executing the parent (B-041: go mod init was re-run 3x and FAILED).
func TestIntakeProcessor_FinishedParentNotRerun(t *testing.T) {
	for _, tt := range []struct {
		name    string
		success bool
		want    models.TaskState
	}{
		{name: "completed stays completed", success: true, want: models.TaskStateCompleted},
		{name: "failed stays failed", success: false, want: models.TaskStateFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runFinishedParentCase(t, tt.success, tt.want)
		})
	}
}

func runFinishedParentCase(t *testing.T, success bool, want models.TaskState) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "go mod init wordcount"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	parent := driveToState(t, ctx, store, tasks[0], success)
	ref := commentAndListRef(t, ctx, store, parent.ID, "add follow-up work please")

	gw := &stubIntakeGateway{structDraft: models.DraftPlan{
		Tasks: []models.DraftTask{{Title: "follow-up one"}, {Title: "follow-up two"}},
	}}
	p := NewIntakeProcessor(store, gw, nil, nil, 0)
	if err := p.Process(ctx, ref); err != nil {
		t.Fatalf("Process: %v", err)
	}
	assertParentUntouched(t, ctx, store, parent.ID, want)
	assertFollowUpsReady(t, ctx, store, parent.ID)
}

func assertParentUntouched(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, parentID string, want models.TaskState) {
	t.Helper()
	after, err := store.GetTask(ctx, parentID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.State != want {
		t.Fatalf("parent state = %s, want %s (parent was re-run)", after.State, want)
	}
}

func assertFollowUpsReady(t *testing.T, ctx context.Context, store *testutil.FakeKanbanStore, parentID string) {
	t.Helper()
	children, err := store.ListChildTasks(ctx, parentID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("children = %d, want 2", len(children))
	}
	for _, child := range children {
		if child.State != models.TaskStateReady {
			t.Errorf("follow-up %q state = %s, want READY (gated behind finished parent)", child.Title, child.State)
		}
		parents, err := store.ListParentTasks(ctx, child.ID)
		if err != nil {
			t.Fatalf("ListParentTasks: %v", err)
		}
		found := false
		for _, par := range parents {
			if par.ID == parentID {
				found = true
			}
		}
		if !found {
			t.Errorf("follow-up %q has no edge back to parent %s (relation direction wrong)", child.Title, parentID)
		}
	}
	blocked, err := store.ListChildTasksByRelation(ctx, parentID, models.TaskRelationBlocks)
	if err != nil {
		t.Fatalf("ListChildTasksByRelation: %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("BLOCKS children = %d, want 2 (edge must run parent -> child)", len(blocked))
	}
}

// Re-processing the same comment must not duplicate follow-ups: after the
// first Process the daemon's listing is empty, so the comment is never
// picked up again (B-041 idempotency).
func TestIntakeProcessor_DuplicateCommentIdempotent(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "done work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	parent := driveToState(t, ctx, store, tasks[0], true)
	ref := commentAndListRef(t, ctx, store, parent.ID, "more work please")

	gw := &stubIntakeGateway{structDraft: models.DraftPlan{
		Tasks: []models.DraftTask{{Title: "follow-up"}},
	}}
	p := NewIntakeProcessor(store, gw, nil, nil, 0)
	if err := p.Process(ctx, ref); err != nil {
		t.Fatalf("first Process: %v", err)
	}
	before := len(store.Tasks())

	refs, err := store.ListUnprocessedHumanComments(ctx)
	if err != nil {
		t.Fatalf("ListUnprocessedHumanComments: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("unprocessed refs after Process = %d, want 0 (duplicate would re-run)", len(refs))
	}
	if got := len(store.Tasks()); got != before {
		t.Fatalf("tasks = %d, want %d (no new tasks without a new comment)", got, before)
	}
}

// Control: a comment on live work keeps the old path — parent re-queues and
// follow-ups wait behind it.
func TestIntakeProcessor_LiveParentStillRequeues(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "live work"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	task := tasks[0]
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 4242)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	ref := commentAndListRef(t, ctx, store, running.ID, "steer the live task")

	gw := &stubIntakeGateway{structDraft: models.DraftPlan{
		Tasks: []models.DraftTask{{Title: "follow-up"}},
	}}
	p := NewIntakeProcessor(store, gw, nil, nil, 0)
	if err := p.Process(ctx, ref); err != nil {
		t.Fatalf("Process: %v", err)
	}
	after, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.State != models.TaskStateReady {
		t.Fatalf("parent state = %s, want READY (live work still re-queues)", after.State)
	}
	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListChildTasks: %v", err)
	}
	if len(children) != 1 || children[0].State != models.TaskStatePending {
		t.Fatalf("children = %#v, want 1 PENDING follow-up gated behind live parent", children)
	}
}
