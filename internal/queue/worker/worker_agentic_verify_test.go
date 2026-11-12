package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// B-082: the builtin agent reported COMPLETED after its own verification had
// failed (a test it wrote fails, or the toolchain was missing). A finish with
// no tool call right after a failed shell command must not be COMPLETED. The
// model can fix a failing command, so the task takes the retry ladder with the
// failure as feedback (T-055b gap 3).
func TestAgenticFinishAfterFailedCommandRetries(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "go test ./..."), {Content: "Done. The feature is complete."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: sandbox.Result{ExitCode: 1, Stderr: "--- FAIL: TestCountLines"}},
	}}
	w, sink, task := runAgenticTask(t, gw, sb)
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateReady || got.RetryCount != 1 {
		t.Fatalf("state = %s retries = %d, want READY with one retry: the last command failed and the task finished without a passing run", got.State, got.RetryCount)
	}
	if !sinkPayloadContains(sink, "RETRY", "TestCountLines") {
		t.Fatalf("events = %#v, want a RETRY carrying the failed command's output", sink.events)
	}
}

// A failed command followed by a passing one is verified: the task completes.
func TestAgenticFinishAfterPassingRerunCompletes(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "go test ./..."), bashCall("c2", "go test ./..."), {Content: "Done. All tests pass."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: sandbox.Result{ExitCode: 1, Stderr: "--- FAIL: TestCountLines"}},
		{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "ok"}},
	}}
	w, _, task := runAgenticTask(t, gw, sb)
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED after a passing rerun", got.State)
	}
}

func TestAgenticTextOnlyFinishWithoutEvidenceIsNotCompleted(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		{Content: "Done. The feature is complete."},
	}}
	sb := &scriptedSandbox{}
	w, _, task := runAgenticDraft(t, gw, sb, models.DraftTask{
		Title: "Inspect host", Description: testutil.AgenticTestTaskDescription(), RequiresWorkspaceChange: true,
	}, nil)
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State == models.TaskStateCompleted {
		t.Fatalf("state = %s, want not COMPLETED: nothing ran and no file changed", got.State)
	}
}
