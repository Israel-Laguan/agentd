package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// refuseTaskSandbox refuses every command of one task with err and runs the
// rest, so a batch can show that one refused task leaves its siblings intact.
type refuseTaskSandbox struct {
	taskID string
	err    error
}

func (s refuseTaskSandbox) Execute(_ context.Context, p sandbox.Payload) (sandbox.Result, error) {
	if p.TaskID == s.taskID {
		return sandbox.Result{ExitCode: -1}, s.err
	}
	return sandbox.Result{Success: true, Stdout: "ok"}, nil
}

func refusedWithoutJail() error {
	return fmt.Errorf("%w: sandbox.backend=bubblewrap is required, but bwrap not found; command refused", models.ErrSandboxUnavailable)
}

// assertRefusedTerminal checks the B-051 contract for a recovery path that hit a
// refusing jail: the task is FAILED with the reason, not parked for a human,
// requeued or handed a permission/prompt handoff.
func assertRefusedTerminal(t *testing.T, w *Worker, sink *mockEventSink, taskID string) {
	t.Helper()
	ctx := context.Background()
	got, err := w.store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailed {
		t.Fatalf("task state = %s, want %s: a refused jail must end the task terminally", got.State, models.TaskStateFailed)
	}
	for _, typ := range []string{"RETRY", string(models.EventTypePermissionDetected)} {
		if n := eventCount(sink, typ); n != 0 {
			t.Fatalf("%s events = %d, want 0", typ, n)
		}
	}
	comments, err := w.store.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 || !strings.Contains(comments[0].Body, "not be retried") {
		t.Fatalf("comments = %#v, want one terminal failure comment", comments)
	}
}

// Permission recovery: `sudo` is blocked at admission, so the worker asks for a
// non-privileged alternative and runs it. The alternative hits the refusing jail.
func TestPermissionRecoveryFailsTerminalWhenSandboxRefuses(t *testing.T) {
	t.Parallel()
	blocked := sandbox.Result{ExitCode: -1, Stderr: "sudo command blocked"}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: blocked, err: fmt.Errorf("%w: sudo is not allowed", models.ErrSandboxViolation)},
		{result: sandbox.Result{ExitCode: -1}, err: refusedWithoutJail()},
	}}
	ctx, w, sink, task, project := newCommandEventWorker(t, sb)
	running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	// routingTestGateway answers the alternative request with {"command":"echo ok"}.
	w.executeLegacyCommand(ctx, *running, project, models.AgentProfile{}, "sudo lscpu", &legacyTaskAudit{})
	assertRefusedTerminal(t, w, sink, task.ID)
}

// Prompt recovery: a prompt hang on `apt-get install` is retried once with -y.
// The retry hits the refusing jail, so it must not become a prompt handoff.
func TestPromptRecoveryFailsTerminalWhenSandboxRefuses(t *testing.T) {
	t.Parallel()
	sb := &scriptedSandbox{steps: []scriptedStep{{result: sandbox.Result{ExitCode: -1}, err: refusedWithoutJail()}}}
	ctx, w, sink, task, project := newCommandEventWorker(t, sb)
	running, err := w.store.MarkTaskRunning(ctx, task.ID, task.UpdatedAt, 1)
	if err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	hung := sandbox.Result{TimedOut: true, Stdout: "Do you want to continue? [Y/n]"}
	w.handlePromptRecovery(ctx, *running, project, "apt-get install foo", hung)
	assertRefusedTerminal(t, w, sink, task.ID)
}

// Batch path: a refused first task must end FAILED on its own, and the second
// task in the same batch must still complete. Both go through processBatchLegacy.
func TestProcessBatchLegacyRefusedTaskFailsAloneSiblingCompletes(t *testing.T) {
	t.Parallel()
	store := &batchTestStore{
		project: models.Project{BaseEntity: models.BaseEntity{ID: "p1"}, WorkspacePath: "/tmp/ws"},
		profile: models.AgentProfile{ID: "ag1", Provider: "openai", Model: "gpt-4", AgenticMode: false},
		tasks:   make(map[string]models.Task),
		results: make(map[string]*models.TaskResult),
	}
	tasks := summarizeTasks(2)
	for _, task := range tasks {
		store.tasks[task.ID] = task
	}
	w := NewWorker(store, &batchTrackingGateway{}, refuseTaskSandbox{taskID: tasks[0].ID, err: refusedWithoutJail()}, nil, nil, WorkerOptions{})

	w.processBatchLegacy(context.Background(), tasks, store.project, store.profile)

	first, second := tasks[0].ID, tasks[1].ID
	if store.tasks[first].State != models.TaskStateFailed {
		t.Fatalf("refused task state = %s, want %s", store.tasks[first].State, models.TaskStateFailed)
	}
	if r := store.results[first]; r == nil || r.Success || !strings.Contains(r.Payload, "command refused") {
		t.Fatalf("refused task result = %#v, want a failed result naming the refusal", r)
	}
	if store.tasks[second].State != models.TaskStateCompleted {
		t.Fatalf("sibling task state = %s, want %s: a refused sibling must not corrupt the batch", store.tasks[second].State, models.TaskStateCompleted)
	}
	if r := store.results[second]; r == nil || !r.Success {
		t.Fatalf("sibling task result = %#v, want success", r)
	}
}

func eventCount(sink *mockEventSink, typ string) int {
	n := 0
	for _, ev := range sink.events {
		if string(ev.Type) == typ {
			n++
		}
	}
	return n
}
