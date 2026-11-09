package worker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/config"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

func goldenFixture(t *testing.T) (*Worker, *testutil.FakeKanbanStore, models.Task, models.Project, models.AgentProfile, *acpReadOnlyLog) {
	t.Helper()
	ctx := context.Background()
	w, store, _, _, _, _, log := acpFixture(t, "unused", false)
	script := fakeagent.Script(report(map[string]any{"summary": "done"}))
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "golden", Tasks: []models.DraftTask{{
		Title: script, Description: "do the thing", SuccessCriteria: []string{"proof exists"},
		GoldenCriteria: []models.GoldenCriterion{{Criterion: "proof exists", CheckCommand: "cat proof"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project.WorkspacePath, 0755); err != nil {
		t.Fatal(err)
	}
	running, err := store.MarkTaskRunning(ctx, tasks[0].ID, tasks[0].UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.GetAgentProfile(ctx, running.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	w.sandbox = &sandbox.BashExecutor{Root: filepath.Dir(project.WorkspacePath), Backend: sandbox.BackendFilterOnly}
	w.sink = &storeEventSink{store: store}
	return w, store, *running, *project, *profile, log
}

func TestBatchResultCannotBypassGoldenCheck(t *testing.T) {
	tasks := summarizeTasks(2)
	tasks[0].SuccessCriteria = []string{"proof exists"}
	tasks[0].GoldenCriteria = []models.GoldenCriterion{{Criterion: "proof exists", CheckCommand: "cat proof"}}
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: t.TempDir()},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", AgenticMode: true},
		tasks:   make(map[string]models.Task), results: make(map[string]*models.TaskResult),
	}
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	w := NewWorker(store, &batchTrackingGateway{}, fixedResultSandbox{result: sandbox.Result{ExitCode: 1, Stderr: "missing"}}, nil, nil,
		WorkerOptions{Batching: config.BatchingConfig{Enabled: true, MaxBatchSize: 2}})
	w.ProcessBatch(context.Background(), tasks)
	store.mu.Lock()
	defer store.mu.Unlock()
	if got := store.tasks[tasks[0].ID].State; got != models.TaskStateReady {
		t.Fatalf("failed slot state = %s, want READY", got)
	}
	if store.results[tasks[0].ID] != nil {
		t.Fatal("failed slot committed a result")
	}
	if got := store.tasks[tasks[1].ID].State; got != models.TaskStateCompleted {
		t.Fatalf("other slot state = %s, want COMPLETED", got)
	}
}

func TestGoldenRepairRetryDoesNotRunInBatch(t *testing.T) {
	ctx := context.Background()
	w, store, task, project, profile, log := goldenFixture(t)
	w.dispatchProcess(ctx, task, project, profile)
	failed := mustGetTask(t, store, task.ID)
	if failed.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", failed.State)
	}
	otherScript := fakeagent.Script(report(map[string]any{"summary": "another task"}))
	other, _, err := store.EnsureProjectTask(ctx, project.ID, models.DraftTask{Title: otherScript, Description: "another task"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project.WorkspacePath, "proof"), []byte("verified"), 0644); err != nil {
		t.Fatal(err)
	}
	gw := &batchTrackingGateway{}
	w.gateway = gw
	w.ProcessBatch(ctx, []models.Task{failed, *other})
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("repair state = %s, want COMPLETED", got)
	}
	if starts, _ := log.runs(); len(starts) != 3 {
		t.Fatalf("ACP runs = %d, want the original plus two individual retries", len(starts))
	}
	if gw.batchCalls != 0 {
		t.Fatalf("batch calls = %d, want 0", gw.batchCalls)
	}
}

func TestACPGoldenFailureAndRepairInWorkspace(t *testing.T) {
	ctx := context.Background()
	w, store, task, project, profile, log := goldenFixture(t)
	w.dispatchProcess(ctx, task, project, profile)
	queued := mustGetTask(t, store, task.ID)
	if queued.State != models.TaskStateReady {
		t.Fatalf("state = %s, want READY", queued.State)
	}
	if len(queued.GoldenCriteria) != 1 {
		t.Fatalf("stored checks = %+v", queued.GoldenCriteria)
	}
	messages := w.buildStageContext(ctx, queued, project, profile)
	var prompt string
	for _, m := range messages {
		prompt += m.Content
	}
	if !strings.Contains(prompt, "cat proof") || !strings.Contains(prompt, "No such file") {
		t.Fatalf("retry prompt lacks command output: %s", prompt)
	}
	if err := os.WriteFile(filepath.Join(project.WorkspacePath, "proof"), []byte("verified\n"), 0644); err != nil {
		t.Fatal(err)
	}
	running, err := store.MarkTaskRunning(ctx, queued.ID, queued.UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.dispatchProcess(ctx, *running, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED", got)
	}
	if starts, _ := log.runs(); len(starts) != 2 {
		t.Fatalf("ACP runs = %d, want 2", len(starts))
	}
}

func TestApprovedDraftRechecksWithoutRunningAgent(t *testing.T) {
	ctx := context.Background()
	w, store, task, project, profile, log := goldenFixture(t)
	profile.RequireReview = true
	w.gateway = &batchTrackingGateway{}
	w.stageAgent = &stageRecorder{output: `{"needs_clarification":false}`}
	if err := os.WriteFile(filepath.Join(project.WorkspacePath, "proof"), []byte("verified"), 0644); err != nil {
		t.Fatal(err)
	}
	w.dispatchProcess(ctx, task, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED", got)
	}
	completeReviewSubtask(t, store, ctx, task.ID)
	queued := requeueParentTask(t, store, ctx, task.ID)
	if err := os.Remove(filepath.Join(project.WorkspacePath, "proof")); err != nil {
		t.Fatal(err)
	}
	running, err := store.MarkTaskRunning(ctx, queued.ID, queued.UpdatedAt, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.dispatchProcess(ctx, *running, project, profile)
	if got := mustGetTask(t, store, task.ID).State; got != models.TaskStateReady {
		t.Fatalf("state = %s, want READY for repair", got)
	}
	if starts, _ := log.runs(); len(starts) != 1 {
		t.Fatalf("ACP runs = %d, want no second run during finalization", len(starts))
	}
	comments, err := store.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	children, err := store.ListChildTasks(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	review := findLatestChildByTitlePrefix(children, models.HITLSubtaskTitleReview)
	if review == nil || !isReviewConsumed(comments, review.ID) {
		t.Fatal("rejected approved draft must not bypass repair on retry")
	}
}

func TestApprovedGoldenDraftPassesWithoutAnotherAgentRun(t *testing.T) {
	ctx := context.Background()
	w, store, task, project, profile, log := goldenFixture(t)
	profile.RequireReview = true
	w.gateway = &batchTrackingGateway{}
	w.stageAgent = &stageRecorder{output: `{"needs_clarification":false}`}
	if err := os.WriteFile(filepath.Join(project.WorkspacePath, "proof"), []byte("verified"), 0644); err != nil {
		t.Fatal(err)
	}
	w.dispatchProcess(ctx, task, project, profile)
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
		t.Fatalf("ACP runs = %d, want 1", len(starts))
	}
}
