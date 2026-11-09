package worker

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"agentd/internal/agent/acp/fakeagent"
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
	if _, err := w.acpExecutionRequest(ctx, models.Task{BaseEntity: models.BaseEntity{ID: "t"}}, models.Project{}, models.AgentProfile{}, stage.AccessWrite); err == nil {
		t.Fatal("retry context built without the previous attempt's handoff")
	}
	store := testutil.NewFakeStore()
	if err := store.AppendEvent(ctx, models.Event{TaskID: sql.NullString{String: "t", Valid: true}, Type: attemptHandoffEvent, Payload: "{broken"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Worker{store: store}).attemptHandoffMessage(ctx, models.Task{BaseEntity: models.BaseEntity{ID: "t"}}); err == nil {
		t.Fatal("corrupt handoff was silently dropped")
	}
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
