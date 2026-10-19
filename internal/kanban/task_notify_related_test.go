package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

// A write can move rows other than the one it was asked about: finishing a task
// unlocks its children and unblocks a parent. Each of those rows changed, so
// each must be announced, or a subscriber to that task's own topic misses it.
func TestStoreNotifiesRowsChangedBySideEffects(t *testing.T) {
	ctx := context.Background()

	t.Run("a successful result unlocks the dependent child", func(t *testing.T) {
		s, rec := notifyingStore(t)
		_, tasks, err := s.MaterializePlan(ctx, models.DraftPlan{
			ProjectName: "chain",
			Tasks: []models.DraftTask{
				{TempID: "a", Title: "A"},
				{TempID: "b", Title: "B", DependsOn: []string{"a"}},
			},
		})
		must(t, err)
		a, b := tasks[0], tasks[1]
		running := seedRunning(t, s, a)
		rec.reset()

		_, err = s.UpdateTaskResult(ctx, a.ID, running.UpdatedAt, models.TaskResult{Success: true, Payload: "ok"})
		must(t, err)

		if !rec.has(b.ID, b.ProjectID) {
			t.Fatalf("child %s was unlocked to READY but not announced; got %+v", b.ID, rec.tasks)
		}
	})

	t.Run("resolving the last child unblocks the parent", func(t *testing.T) {
		s, rec := notifyingStore(t)
		parent := seedTestTask(t, s, "unblock-parent", models.TaskStateRunning)
		_, children, err := s.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{Title: "child", Description: "d"}})
		must(t, err)
		child := seedRunning(t, s, children[0])
		rec.reset()

		_, err = s.UpdateTaskState(ctx, child.ID, child.UpdatedAt, models.TaskStateCompleted)
		must(t, err)

		if !rec.has(parent.ID, parent.ProjectID) {
			t.Fatalf("parent %s was unblocked to READY but not announced; got %+v", parent.ID, rec.tasks)
		}
	})
}

func seedRunning(t *testing.T, s *Store, task models.Task) models.Task {
	t.Helper()
	ctx := context.Background()
	if task.State == models.TaskStateReady {
		queued, err := s.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateQueued)
		must(t, err)
		task = *queued
	}
	running, err := s.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateRunning)
	must(t, err)
	return *running
}

// MarkProjectTasksReady returns every READY task of the project, but only the
// PENDING ones it just unlocked changed; announcing the rest is noise.
func TestMarkProjectTasksReadyNotifiesOnlyUnlockedTasks(t *testing.T) {
	ctx := context.Background()
	s, rec := notifyingStore(t)
	project, tasks, err := s.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "pending", WorkspacePending: true,
		Tasks: []models.DraftTask{{TempID: "a", Title: "A"}, {TempID: "b", Title: "B"}},
	})
	must(t, err)
	rec.reset()

	_, err = s.MarkProjectTasksReady(ctx, project.ID)
	must(t, err)
	for _, task := range tasks {
		if !rec.has(task.ID, project.ID) {
			t.Fatalf("unlocked task %s not announced; got %+v", task.ID, rec.tasks)
		}
	}

	rec.reset()
	if _, err := s.MarkProjectTasksReady(ctx, project.ID); err != nil {
		t.Fatal(err)
	}
	if len(rec.tasks) != 0 {
		t.Fatalf("second call changed nothing but announced %+v", rec.tasks)
	}
}

// The HITL timeout also fails the parent's open children.
func TestReconcileExpiredBlockedTasksNotifiesFailedChildren(t *testing.T) {
	ctx := context.Background()
	s, rec := notifyingStore(t)
	parent := seedTestTask(t, s, "hitl-notify", models.TaskStateRunning)
	expired := time.Now().Add(-time.Minute)
	must(t, s.AddComment(ctx, models.Comment{
		TaskID: parent.ID, Author: models.CommentAuthorWorkerAgent,
		Body: models.HITLExpiresAtCommentPrefix + expired.UTC().Format(time.RFC3339),
	}))
	blocked, children, err := s.BlockTaskWithSubtasks(ctx, parent.ID, parent.UpdatedAt, []models.DraftTask{{
		Title: "Approve", Description: "review", Assignee: models.TaskAssigneeHuman,
	}})
	must(t, err)
	rec.reset()

	_, err = s.ReconcileExpiredBlockedTasks(ctx, time.Now())
	must(t, err)

	if !rec.has(blocked.ID, blocked.ProjectID) {
		t.Fatalf("expired parent not announced; got %+v", rec.tasks)
	}
	if !rec.has(children[0].ID, children[0].ProjectID) {
		t.Fatalf("child %s was failed by the timeout but not announced; got %+v", children[0].ID, rec.tasks)
	}
}
