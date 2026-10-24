package worker

import (
	"context"
	"reflect"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

type scriptedStep struct {
	result sandbox.Result
	err    error
}

// scriptedSandbox replays one step per Execute call and then succeeds.
type scriptedSandbox struct{ steps []scriptedStep }

func (s *scriptedSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	if len(s.steps) == 0 {
		return sandbox.Result{Success: true}, nil
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	return step.result, step.err
}

func newCommandEventWorker(t *testing.T, sb sandbox.Executor) (context.Context, *Worker, *mockEventSink, models.Task, models.Project) {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "cmd-paths", "inspect host")
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	sink := &mockEventSink{}
	return ctx, NewWorker(store, &routingTestGateway{}, sb, nil, sink, WorkerOptions{}), sink, task, *project
}

func assertCommandEvents(t *testing.T, sink *mockEventSink, want ...string) {
	t.Helper()
	if got := commandEvents(sink); !reflect.DeepEqual(got, want) {
		t.Fatalf("COMMAND events = %#v, want %#v", got, want)
	}
}

// The permission-recovery alternative is a second command that ran; both must be traceable.
func TestRecoverLegacyPrivilegeRecordsAlternativeCommand(t *testing.T) {
	t.Parallel()
	sb := &scriptedSandbox{steps: []scriptedStep{{result: sandbox.Result{Success: true, Stdout: "ok"}}}}
	ctx, w, sink, task, project := newCommandEventWorker(t, sb)
	// routingTestGateway answers every Generate call with {"command":"echo ok"}.
	w.recoverLegacyPrivilege(ctx, task, project, models.AgentProfile{}, "sudo lscpu", &legacyTaskAudit{})
	assertCommandEvents(t, sink, "echo ok")
}

func TestHandlePromptRecoveryRecordsRecoveredCommand(t *testing.T) {
	t.Parallel()
	sb := &scriptedSandbox{steps: []scriptedStep{{result: sandbox.Result{Success: true, Stdout: "done"}}}}
	ctx, w, sink, task, project := newCommandEventWorker(t, sb)
	hung := sandbox.Result{TimedOut: true, Stdout: "Do you want to continue? [Y/n]"}
	w.handlePromptRecovery(ctx, task, project, "apt-get install foo", hung)
	assertCommandEvents(t, sink, "apt-get -y install foo")
}
