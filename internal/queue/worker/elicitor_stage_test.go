package worker

import (
	"context"
	"errors"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/stage"
	"agentd/internal/testutil"
)

const needsClarificationJSON = `{"needs_clarification":true,"questions":[{"question":"Which env?"}],"reason":"vague"}`

func newElicitationFixture(t *testing.T, agent *stageRecorder, opts WorkerOptions) (*Worker, models.Task, models.Project) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, tasks, err := store.MaterializePlan(ctx, models.DraftPlan{
		ProjectName: "P", Description: "d",
		Tasks: []models.DraftTask{{Title: "Fix bug", Description: "fix the bug"}},
	})
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	w := NewWorker(store, nil, nil, nil, nil, opts)
	w.stageAgent = agent
	return w, tasks[0], *project
}

func TestElicitorMakesOneTaskCreationCall(t *testing.T) {
	t.Parallel()
	agent := &stageRecorder{output: `{"needs_clarification":false}`}
	w, task, project := newElicitationFixture(t, agent, WorkerOptions{})

	if _, _, err := w.RunPreTaskElicitation(context.Background(), task, project); err != nil {
		t.Fatal(err)
	}

	if len(agent.reqs) != 1 {
		t.Fatalf("agent calls = %d, want exactly 1", len(agent.reqs))
	}
	req := agent.reqs[0]
	if req.Stage != stage.StageTaskCreation || req.Purpose != gateway.PurposeCreateElicit {
		t.Errorf("call = stage %q purpose %q, want task creation and elicit", req.Stage, req.Purpose)
	}
	if req.Temperature != 0.1 {
		t.Errorf("temperature %v, want 0.1", req.Temperature)
	}
	if agent.taskIDs[0] != task.ID {
		t.Errorf("task id in ctx = %q, want %q", agent.taskIDs[0], task.ID)
	}
}

// The elicitor records no tokens today; uniform accounting is a separate change.
func TestElicitorRecordsNoTokens(t *testing.T) {
	t.Parallel()
	ts := &fakeTokenStore{}
	agent := &stageRecorder{output: `{"needs_clarification":false}`, tokens: 50}
	w, task, project := newElicitationFixture(t, agent, WorkerOptions{TokenStore: ts})

	if _, _, err := w.RunPreTaskElicitation(context.Background(), task, project); err != nil {
		t.Fatal(err)
	}
	if ts.callCount() != 0 {
		t.Fatalf("token store calls = %d, want 0", ts.callCount())
	}
}

func TestElicitorErrorIsSwallowedAndTheTaskContinues(t *testing.T) {
	t.Parallel()
	agent := &stageRecorder{err: errors.New("provider down")}
	w, task, project := newElicitationFixture(t, agent, WorkerOptions{})

	got, blocked, err := w.RunPreTaskElicitation(context.Background(), task, project)
	if err != nil || blocked || got.ID != task.ID {
		t.Fatalf("got task %q blocked=%v err=%v, want the task to continue", got.ID, blocked, err)
	}
}

func TestElicitorQuestionsBlockTheTask(t *testing.T) {
	t.Parallel()
	agent := &stageRecorder{output: needsClarificationJSON}
	w, task, project := newElicitationFixture(t, agent, WorkerOptions{})

	_, blocked, err := w.RunPreTaskElicitation(context.Background(), task, project)
	if err != nil || !blocked {
		t.Fatalf("blocked=%v err=%v, want the task blocked for clarification", blocked, err)
	}
}
