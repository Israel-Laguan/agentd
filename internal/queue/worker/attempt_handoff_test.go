package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
	"agentd/internal/queue/worker/agentic"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

type handoffWriteFailureStore struct{ models.KanbanStore }

func (handoffWriteFailureStore) AppendEvent(context.Context, models.Event) error {
	return errors.New("disk unavailable")
}

type handoffReadFailureStore struct{ models.KanbanStore }

func (handoffReadFailureStore) ListEventsByTask(context.Context, string) ([]models.Event, error) {
	return nil, errors.New("disk unavailable")
}

func TestUnreadableHandoffStopsRetry(t *testing.T) {
	ctx := context.Background()
	w := &Worker{store: handoffReadFailureStore{KanbanStore: testutil.NewFakeStore()}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t"}}
	if _, err := w.acpExecutionRequest(ctx, task, models.Project{}, models.AgentProfile{}, stage.AccessWrite); !errors.Is(err, errAttemptHandoffUnavailable) {
		t.Fatalf("ACP retry context built without the previous attempt's handoff: %v", err)
	}
	if _, err := w.AssembleAgenticSystemPrompt(ctx, task, models.Project{}, models.AgentProfile{}); !errors.Is(err, errAttemptHandoffUnavailable) {
		t.Fatalf("builtin retry context built without the previous attempt's handoff: %v", err)
	}
	store := testutil.NewFakeStore()
	if err := store.AppendEvent(ctx, models.Event{TaskID: sql.NullString{String: "t", Valid: true}, Type: attemptHandoffEvent, Payload: "{broken"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Worker{store: store}).attemptHandoffMessage(ctx, models.Task{BaseEntity: models.BaseEntity{ID: "t"}}, models.Project{}); err == nil {
		t.Fatal("corrupt handoff was silently dropped")
	}
}

// An unreadable note fails the attempt as a counted retry before any agent
// runs, and the note it could not read is kept for the next try.
func TestUnreadableHandoffRetriesWithoutAgentOrOverwrite(t *testing.T) {
	ctx := context.Background()
	const corrupt = "{broken"
	lastNote := func(t *testing.T, store *testutil.FakeKanbanStore, task models.Task) string {
		events, err := store.ListEventsByTask(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		note := ""
		for _, e := range events {
			if e.Type == attemptHandoffEvent {
				note = e.Payload
			}
		}
		return note
	}
	check := func(t *testing.T, store *testutil.FakeKanbanStore, task models.Task) {
		t.Helper()
		if got := lastNote(t, store, task); got != corrupt {
			t.Fatalf("handoff note replaced with %q", got)
		}
		after, err := store.GetTask(ctx, task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if after.State == models.TaskStateFailedRequiresHuman || after.State == models.TaskStateCompleted {
			t.Fatalf("task state = %s, want a counted retry", after.State)
		}
	}
	seed := func(t *testing.T, store *testutil.FakeKanbanStore, task models.Task) {
		if err := store.AppendEvent(ctx, models.Event{ProjectID: task.ProjectID, TaskID: sql.NullString{String: task.ID, Valid: true}, Type: attemptHandoffEvent, Payload: corrupt}); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("builtin", func(t *testing.T) {
		store, task, project, _ := acpTask(t, "Fix tests", "d", false)
		seed(t, store, task)
		gw := &agenticHostGateway{}
		NewWorker(store, gw, panicSandbox{t}, nil, &mockEventSink{}, WorkerOptions{}).
			dispatchProcess(ctx, task, project, models.AgentProfile{AgenticMode: true, Provider: "openai"})
		for _, req := range gw.requests {
			if req.Purpose == gateway.PurposeExecTurn {
				t.Fatal("agent turn ran without the previous attempt's handoff")
			}
		}
		check(t, store, task)
	})
	t.Run("acp", func(t *testing.T) {
		w, store, task, project, profile, _, _ := acpFixture(t, fakeagent.Script(fakeagent.Step{Error: "agent must not run"}), false)
		seed(t, store, task)
		w.dispatchProcess(ctx, task, project, profile)
		check(t, store, task)
	})
}

// The note is protected from shedding, so its worst case must leave room in
// the ACP budget for the system and task messages.
func TestAttemptHandoffIsBounded(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	w := &Worker{store: store}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t"}}
	huge := strings.Repeat("x", 100_000)
	var tools, files []string
	for i := range 40 {
		tools = append(tools, huge)
		files = append(files, fmt.Sprintf("%d/%s", i, huge))
	}
	res := stage.StageResult{Progress: huge, Plan: huge, FailedTools: tools, FilesTouched: append(files, files...)}
	if err := w.writeAttemptHandoff(ctx, task, res, errors.New(huge)); err != nil {
		t.Fatal(err)
	}
	msg, err := w.attemptHandoffMessage(ctx, task, models.Project{OriginalInput: huge})
	if err != nil || msg == nil {
		t.Fatalf("message = %v, error = %v", msg, err)
	}
	if len(msg.Content) > acpContextBudget*3/4 {
		t.Fatalf("handoff is %d bytes, over 3/4 of the %d-byte budget", len(msg.Content), acpContextBudget)
	}
	for _, want := range []string{"36 more failed calls omitted", "30 more files omitted"} {
		if !strings.Contains(msg.Content, want) {
			t.Errorf("handoff does not say %q", want)
		}
	}
}

// An attempt whose context did not fit never ran, so the last note is kept.
func TestOverBudgetAttemptKeepsLastHandoff(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	w := &Worker{store: store}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t"}}
	if err := w.writeAttemptHandoff(ctx, task, stage.StageResult{Progress: "edited handler.go"}, errors.New("tests failed")); err != nil {
		t.Fatal(err)
	}
	_, err := fitStageContext([]spec.PromptMessage{{Role: "system", Section: spec.SectionSystem, Content: strings.Repeat("s", 100)}}, 10)
	if !w.saveACPHandoff(ctx, task, models.Project{}, stage.StageResult{}, err) {
		t.Fatal("over-budget attempt failed the task")
	}
	msg, err := w.attemptHandoffMessage(ctx, task, models.Project{})
	if err != nil || msg == nil || !strings.Contains(msg.Content, "edited handler.go") {
		t.Fatalf("last handoff lost: %v, %v", msg, err)
	}
}

// A loop error also ends with ResultNone but leaves the task RUNNING; only a
// task handed to a human skips the note.
func TestStoppedAtHumanUsesStoredState(t *testing.T) {
	ctx := context.Background()
	store, task, _, _ := acpTask(t, "Fix tests", "d", false)
	w := &Worker{store: store}
	if w.stoppedAtHuman(ctx, task.ID, agentic.Result{}) {
		t.Fatal("a RUNNING task after a loop error counted as a human stop")
	}
	if !w.stoppedAtHuman(ctx, task.ID, agentic.Result{Kind: agentic.ResultPromptHandoff}) {
		t.Fatal("prompt handoff did not count as a human stop")
	}
	if _, err := store.UpdateTaskState(ctx, task.ID, task.UpdatedAt, models.TaskStateBlocked); err != nil {
		t.Fatal(err)
	}
	if !w.stoppedAtHuman(ctx, task.ID, agentic.Result{}) {
		t.Fatal("a BLOCKED task did not count as a human stop")
	}
}

func TestAttemptHandoffReportsPersistenceFailure(t *testing.T) {
	w := &Worker{store: handoffWriteFailureStore{KanbanStore: testutil.NewFakeStore()}}
	if err := w.writeAttemptHandoff(context.Background(), models.Task{}, stage.StageResult{}, errors.New("failed")); err == nil {
		t.Fatal("lost handoff was not reported")
	}
}

func TestFailedAttemptHandoffSurvivesWorkerRestart(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	_, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{ProjectName: "handoff", Tasks: []models.DraftTask{{Title: "Fix tests", Description: "Do not push. Keep this wording!"}}})
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	w := &Worker{store: store}
	original := "Do not push.\nKeep 'quotes' and <symbols> verbatim."
	if err := w.writeAttemptHandoff(ctx, task, stage.StageResult{Progress: "Changed handler.go; chose a smaller patch; run integration tests."}, errors.New("go test ./...: exact failure")); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEventsByTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Type == attemptHandoffEvent && strings.Contains(e.Payload, "Keep 'quotes'") {
			t.Fatalf("event log holds the user's request: %s", e.Payload)
		}
	}
	other := &Worker{store: store}
	messages, err := other.buildStageContext(ctx, task, models.Project{OriginalInput: original}, models.AgentProfile{})
	if err != nil {
		t.Fatal(err)
	}
	text := renderContext(messages)
	for _, want := range []string{"Fix tests", "Do not push. Keep this wording!", original, "handler.go", "go test ./...: exact failure", "run integration tests"} {
		if !strings.Contains(text, want) {
			t.Errorf("next context missing %q: %s", want, text)
		}
	}
	other.clearAttemptHandoff(ctx, task)
	messages, err = other.buildStageContext(ctx, task, models.Project{}, models.AgentProfile{})
	if err != nil {
		t.Fatal(err)
	}
	text = renderContext(messages)
	if strings.Contains(text, "Previous failed attempt handoff") {
		t.Fatal("successful attempt left a stale handoff in the next context")
	}
}

func TestFakeACPAgentReceivesFailedAttemptHandoff(t *testing.T) {
	ctx := context.Background()
	script := fakeagent.Script(fakeagent.Step{Message: "Ran go test ./...: FAIL exact failure. Edited handler.go; integration test remains."}, fakeagent.Step{Error: "attempt failed"})
	w, store, task, project, profile, _, _ := acpFixture(t, script, false)
	task.Description = "Keep the user constraint verbatim."
	w.dispatchProcess(ctx, task, project, profile)

	// Build the next request with a new worker, as after a daemon restart.
	next := &Worker{store: store}
	task.Title = fakeagent.Script(fakeagent.Step{Tool: &fakeagent.Tool{Name: "get_context"}}, report(map[string]any{"name": "x"}))
	req, err := next.acpExecutionRequest(ctx, task, project, profile, stage.AccessWrite)
	if err != nil {
		t.Fatal(err)
	}
	req.Contract = stage.NewContract[acpOut]()
	res, err := fakeACPAgent(t, nil).Call(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Previous failed attempt handoff", "go test ./...", "FAIL exact failure", "handler.go", "integration test remains", "Keep the user constraint verbatim."} {
		if !strings.Contains(res.Progress, want) {
			t.Errorf("fake agent did not receive %q: %s", want, res.Progress)
		}
	}
}
