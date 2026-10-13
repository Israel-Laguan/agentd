package kanban

import (
	"context"
	"testing"

	"agentd/internal/models"
)

// driveTerminal moves a task to a terminal state through the realistic
// ladder so CompletedAt/assignee match production rows.
func driveTerminal(t *testing.T, store *Store, ctx context.Context, task models.Task, next models.TaskState) models.Task {
	t.Helper()
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 4242)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	success := next == models.TaskStateCompleted
	done, err := store.UpdateTaskResult(ctx, task.ID, running.UpdatedAt, models.TaskResult{Success: success})
	if err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}
	if next == models.TaskStateFailedRequiresHuman {
		moved, err := store.UpdateTaskState(ctx, done.ID, done.UpdatedAt, models.TaskStateFailedRequiresHuman)
		if err != nil {
			t.Fatalf("UpdateTaskState: %v", err)
		}
		return *moved
	}
	if done.State != next {
		t.Fatalf("state = %s, want %s", done.State, next)
	}
	return *done
}

// B-041: a human comment on a finished task must not yank it back into
// IN_CONSIDERATION (intake re-ran `go mod init` until it FAILED).
func TestAddComment_TerminalTaskKeepsState(t *testing.T) {
	for _, want := range []models.TaskState{
		models.TaskStateCompleted,
		models.TaskStateFailed,
		models.TaskStateFailedRequiresHuman,
	} {
		t.Run(string(want), func(t *testing.T) {
			store := newTestStore(t)
			ctx := context.Background()
			_, tasks, err := store.MaterializePlan(ctx, samplePlan())
			if err != nil || len(tasks) == 0 {
				t.Fatalf("materialize: %v", err)
			}
			terminal := driveTerminal(t, store, ctx, tasks[0], want)
			if err := store.AddComment(ctx, models.Comment{
				TaskID: terminal.ID,
				Author: models.CommentAuthorUser,
				Body:   "follow-up work please",
			}); err != nil {
				t.Fatalf("AddComment: %v", err)
			}
			after, err := store.GetTask(ctx, terminal.ID)
			if err != nil {
				t.Fatalf("GetTask: %v", err)
			}
			if after.State != want {
				t.Fatalf("state = %s, want %s (finished task was re-queued)", after.State, want)
			}
			if after.Assignee != models.TaskAssigneeHuman {
				t.Fatalf("assignee = %s, want HUMAN (intake must still find the comment)", after.Assignee)
			}
		})
	}
}

// B-041: intake must discover human comments on finished tasks, and each
// comment must surface exactly once until marked processed.
func TestListUnprocessed_IncludesTerminalTasks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, tasks, err := store.MaterializePlan(ctx, samplePlan())
	if err != nil || len(tasks) == 0 {
		t.Fatalf("materialize: %v", err)
	}
	terminal := driveTerminal(t, store, ctx, tasks[0], models.TaskStateCompleted)
	if err := store.AddComment(ctx, models.Comment{
		TaskID: terminal.ID,
		Author: models.CommentAuthorUser,
		Body:   "follow-up work please",
	}); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	refs, err := store.ListUnprocessedHumanComments(ctx)
	if err != nil {
		t.Fatalf("ListUnprocessedHumanComments: %v", err)
	}
	if len(refs) != 1 || refs[0].TaskID != terminal.ID {
		t.Fatalf("refs = %#v, want 1 ref for the finished task", refs)
	}
	if err := store.MarkCommentProcessed(ctx, terminal.ID, refs[0].CommentEventID); err != nil {
		t.Fatalf("MarkCommentProcessed: %v", err)
	}
	refs, err = store.ListUnprocessedHumanComments(ctx)
	if err != nil {
		t.Fatalf("ListUnprocessedHumanComments: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs after mark = %#v, want empty (duplicate comment idempotent)", refs)
	}
}
