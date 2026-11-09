package worker

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
	"agentd/internal/gateway"
	"agentd/internal/models"
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
	if _, err := (&Worker{store: store}).attemptHandoffMessage(ctx, models.Task{BaseEntity: models.BaseEntity{ID: "t"}}); err == nil {
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

func TestAttemptHandoffReportsPersistenceFailure(t *testing.T) {
	w := &Worker{store: handoffWriteFailureStore{KanbanStore: testutil.NewFakeStore()}}
	if err := w.writeAttemptHandoff(context.Background(), models.Task{}, models.Project{}, stage.StageResult{}, errors.New("failed")); err == nil {
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
	if err := w.writeAttemptHandoff(ctx, task, models.Project{OriginalInput: original}, stage.StageResult{Progress: "Changed handler.go; chose a smaller patch; run integration tests."}, errors.New("go test ./...: exact failure")); err != nil {
		t.Fatal(err)
	}
	other := &Worker{store: store}
	messages, err := other.buildStageContext(ctx, task, models.Project{}, models.AgentProfile{})
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
