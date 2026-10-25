package worker

import (
	"context"
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
