package recovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"agentd/internal/models"
	"agentd/internal/queue/safety"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// B-010 / T-036b: with recovery.clean_workspace_on_recover on, boot reconcile
// resets the workspace of a project that started empty, so the re-run does not
// see the interrupted attempt's files. The reset is guarded: it never deletes
// output it cannot account for.

type resetFixture struct {
	store     *testutil.FakeKanbanStore
	ws        *sandbox.FSWorkspaceManager
	sink      *recordingSink
	project   models.Project
	taskIDs   []string
	partial   string
	projectID string
}

// deadPID is owned by no live process in these tests.
const deadPID = 999999

// newResetFixture materializes a project with one task per title, marks the first
// RUNNING under a dead PID, and has that attempt leave a partial file behind.
func newResetFixture(t *testing.T, startEmpty bool, titles ...string) *resetFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store := testutil.NewFakeStore()
	store.SetProjectsDir(root)
	drafts := make([]models.DraftTask, 0, len(titles))
	for _, title := range titles {
		drafts = append(drafts, models.DraftTask{TempID: title, Title: title})
	}
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "reset", StartEmptyWorkspace: startEmpty, Tasks: drafts,
	})
	if err != nil {
		t.Fatalf("MaterializePlan: %v", err)
	}
	ws := &sandbox.FSWorkspaceManager{Root: root}
	dir, err := ws.EnsureProjectDir(ctx, project.ID)
	if err != nil {
		t.Fatalf("EnsureProjectDir: %v", err)
	}
	partial := filepath.Join(dir, "first-attempt.txt")
	if err := os.WriteFile(partial, []byte("partial"), 0o644); err != nil {
		t.Fatalf("write partial: %v", err)
	}
	f := &resetFixture{store: store, ws: ws, sink: &recordingSink{}, project: *project, partial: partial, projectID: project.ID}
	for _, task := range tasks {
		f.taskIDs = append(f.taskIDs, task.ID)
	}
	f.markRunning(t, f.taskIDs[0], deadPID)
	return f
}

func (f *resetFixture) markRunning(t *testing.T, id string, pid int) {
	t.Helper()
	ctx := context.Background()
	task, err := f.store.GetTask(ctx, id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := f.store.MarkTaskRunning(ctx, id, task.UpdatedAt, pid); err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
}

func (f *resetFixture) boot(t *testing.T, opts ...BootOption) error {
	t.Helper()
	return BootReconcile(context.Background(), f.store, safety.StaticPIDProbe{PIDs: []int{1}}, f.sink, opts...)
}

func (f *resetFixture) state(t *testing.T, id string) models.TaskState {
	t.Helper()
	task, err := f.store.GetTask(context.Background(), id)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return task.State
}

func (f *resetFixture) partialExists() bool {
	_, err := os.Stat(f.partial)
	return err == nil
}

func (f *resetFixture) hasEvent(eventType models.EventType, taskID string) bool {
	for _, ev := range f.sink.events {
		if ev.Type == eventType && ev.TaskID.String == taskID {
			return true
		}
	}
	return false
}

func TestBootReconcile_resetsStartedEmptyWorkspace(t *testing.T) {
	f := newResetFixture(t, true, "a")

	if err := f.boot(t, WithWorkspaceReset(f.ws)); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if f.partialExists() {
		t.Fatal("the interrupted attempt's file survived the reset")
	}
	if _, err := os.Stat(f.ws.ProjectDir(f.projectID)); err != nil {
		t.Fatalf("project dir must still exist for the re-run: %v", err)
	}
	if got := f.state(t, f.taskIDs[0]); got != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", got)
	}
	if !f.hasEvent(models.EventTypeRecoveryWorkspaceReset, f.taskIDs[0]) {
		t.Fatalf("no workspace reset event: %#v", f.sink.events)
	}
}

func TestBootReconcile_keepsWorkspaceWhenFlagOff(t *testing.T) {
	f := newResetFixture(t, true, "a")

	if err := f.boot(t); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if !f.partialExists() {
		t.Fatal("the workspace was reset with the flag off")
	}
	if got := f.state(t, f.taskIDs[0]); got != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", got)
	}
}

func TestBootReconcile_resetsOncePerProjectForSeveralRecoveredTasks(t *testing.T) {
	f := newResetFixture(t, true, "a", "b")
	f.markRunning(t, f.taskIDs[1], deadPID)
	resetter := &countingResetter{WorkspaceResetter: f.ws}

	if err := f.boot(t, WithWorkspaceReset(resetter)); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if f.partialExists() {
		t.Fatal("the interrupted attempts' file survived the reset")
	}
	if resetter.calls != 1 {
		t.Fatalf("ResetProjectDir calls = %d, want 1 per project", resetter.calls)
	}
	for _, id := range f.taskIDs {
		if got := f.state(t, id); got != models.TaskStateReady {
			t.Fatalf("task %s state = %s, want READY", id, got)
		}
	}
}

func TestBootReconcile_refusesResetForProjectNotStartedEmpty(t *testing.T) {
	f := newResetFixture(t, false, "a")

	if err := f.boot(t, WithWorkspaceReset(f.ws)); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if !f.partialExists() {
		t.Fatal("a seeded project's workspace was deleted")
	}
	f.assertRefused(t, f.taskIDs[0])
}

func TestBootReconcile_refusesResetWhenProjectHasCompletedTask(t *testing.T) {
	f := newResetFixture(t, true, "a", "done")
	ctx := context.Background()
	done := f.taskIDs[1]
	task, err := f.store.GetTask(ctx, done)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if _, err := f.store.UpdateTaskResult(ctx, done, task.UpdatedAt, models.TaskResult{Success: true, Payload: "ok"}); err != nil {
		t.Fatalf("UpdateTaskResult: %v", err)
	}

	if err := f.boot(t, WithWorkspaceReset(f.ws)); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if !f.partialExists() {
		t.Fatal("output of a COMPLETED sibling was deleted")
	}
	f.assertRefused(t, f.taskIDs[0])
	if got := f.state(t, done); got != models.TaskStateCompleted {
		t.Fatalf("completed sibling state = %s, want COMPLETED", got)
	}
}

func TestBootReconcile_refusesResetWhenSiblingStillRunningElsewhere(t *testing.T) {
	f := newResetFixture(t, true, "a", "other")
	// A second live daemon owns the sibling: its PID is alive, so it stays RUNNING.
	f.markRunning(t, f.taskIDs[1], 4242)

	err := BootReconcile(context.Background(), f.store,
		safety.StaticPIDProbe{PIDs: []int{1, 4242}}, f.sink, WithWorkspaceReset(f.ws))
	if err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	if !f.partialExists() {
		t.Fatal("workspace of a project with a RUNNING sibling was deleted")
	}
	f.assertRefused(t, f.taskIDs[0])
}

type countingResetter struct {
	WorkspaceResetter
	calls int
}

func (c *countingResetter) ResetProjectDir(ctx context.Context, projectID string) error {
	c.calls++
	return c.WorkspaceResetter.ResetProjectDir(ctx, projectID)
}

type failingResetter struct{}

func (failingResetter) ResetProjectDir(context.Context, string) error {
	return errors.New("disk on fire")
}

func TestBootReconcile_failsTaskWhenResetErrors(t *testing.T) {
	f := newResetFixture(t, true, "a")

	if err := f.boot(t, WithWorkspaceReset(failingResetter{})); err != nil {
		t.Fatalf("BootReconcile() error = %v", err)
	}

	f.assertRefused(t, f.taskIDs[0])
}

func (f *resetFixture) assertRefused(t *testing.T, id string) {
	t.Helper()
	if got := f.state(t, id); got != models.TaskStateFailedRequiresHuman {
		t.Fatalf("recovered task state = %s, want FAILED_REQUIRES_HUMAN (refusal must be loud)", got)
	}
	if !f.hasEvent(models.EventTypeRecoveryResetRefused, id) {
		t.Fatalf("no refusal event for %s: %#v", id, f.sink.events)
	}
}
