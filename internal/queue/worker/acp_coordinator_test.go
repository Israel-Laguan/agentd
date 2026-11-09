package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/models"
)

func TestACPClarifiesBeforeStartingSession(t *testing.T) {
	ctx := context.Background()
	w, store, task, project, profile, _, log := acpFixture(t, "Fix bug", false)
	w.stageAgent = &stageRecorder{output: needsClarificationJSON}
	w.dispatchProcess(ctx, task, project, profile)
	got := mustGetTask(t, store, task.ID)
	if got.State != models.TaskStateBlocked {
		t.Errorf("state = %s, want BLOCKED", got.State)
	}
	if starts, _ := log.runs(); len(starts) != 0 {
		t.Errorf("ACP sessions = %d, want 0", len(starts))
	}
	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil || len(children) != 1 || !strings.Contains(children[0].Title, "Which env?") {
		t.Fatalf("clarification subtasks = %+v, %v; want the elicited question", children, err)
	}
}

func TestACPSkipsClarificationWithBuiltinCriteriaRules(t *testing.T) {
	ctx := context.Background()
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, log := acpFixture(t, script, false)
	task.Description = strings.Repeat("Detailed requirements for the task. ", 20)
	task.SuccessCriteria = []string{"the task is complete"}
	elicitor := &stageRecorder{output: needsClarificationJSON}
	w.stageAgent = elicitor
	w.dispatchProcess(ctx, task, project, profile)
	if len(elicitor.reqs) != 0 {
		t.Fatalf("clarification calls = %d, want 0", len(elicitor.reqs))
	}
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", got)
	}
	if starts, _ := log.runs(); len(starts) != 1 {
		t.Fatalf("ACP sessions = %d, want 1", len(starts))
	}
}

func TestACPCompletionChecksSuccessCriteria(t *testing.T) {
	ctx := context.Background()
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, _ := acpFixture(t, script, false)
	task.SuccessCriteria = []string{"missing.go exists"}
	w.dispatchProcess(ctx, task, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN", got)
	}
}

func TestACPCompletionChecksOpenBreakdown(t *testing.T) {
	ctx := context.Background()
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	w, store, task, project, profile, _, _ := acpFixture(t, script, false)
	_, _, err := store.BlockTaskWithSubtasks(ctx, task.ID, task.UpdatedAt, []models.DraftTask{{Title: "open subtask", SplitChild: true}})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.dispatchProcess(ctx, *running, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateFailedRequiresHuman {
		t.Fatalf("state = %s, want FAILED_REQUIRES_HUMAN", got)
	}
}

func TestACPReviewFinalizesWithoutSecondRun(t *testing.T) {
	ctx := context.Background()
	script := fakeagent.Script(report(map[string]any{"summary": "draft from ACP"}))
	w, store, task, project, profile, _, log := acpFixture(t, script, false)
	profile.RequireReview = true
	w.gateway = &batchTrackingGateway{}
	w.stageAgent = &stageRecorder{output: `{"needs_clarification":false}`}
	path := withAuditFile(t, w)
	w.dispatchProcess(ctx, task, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED for review", got)
	}
	if starts, _ := log.runs(); len(starts) != 1 {
		t.Fatalf("ACP sessions = %d, want 1", len(starts))
	}
	if got := auditTypesFor(t, path, task.ID); len(got) != 2 || got[1] != "task_review" {
		t.Fatalf("audit records = %v, want [task_start task_review]", got)
	}
	completeReviewSubtask(t, store, ctx, task.ID)
	queued := requeueParentTask(t, store, ctx, task.ID)
	running, err := store.MarkTaskRunning(ctx, queued.ID, queued.UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.dispatchProcess(ctx, *running, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", got)
	}
	if starts, _ := log.runs(); len(starts) != 1 {
		t.Fatalf("ACP sessions = %d, want no second run", len(starts))
	}
}
