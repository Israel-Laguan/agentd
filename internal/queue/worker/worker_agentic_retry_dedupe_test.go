package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// retryDraft is the task both attempts run. It has no success criteria, so only
// the retry rule is under test.
func retryDraft() models.DraftTask {
	return models.DraftTask{Title: "Fix the tests", Description: testutil.AgenticTestTaskDescription()}
}

// B-043 on the agentic path: a retry that re-sends the command that failed first
// in the previous attempt does not run it blind. The failure is fed back.
func TestAgenticRetryRefusesCommandThatFailedFirstInPreviousAttempt(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "make test"), {Content: "Done."},
		bashCall("c2", "make test"), {Content: "Done."},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"make test": {Success: false, ExitCode: 1, Stderr: "--- FAIL: TestCountLines"},
	}}
	w, _, task, project, profile := agenticFixture(t, gw, sb, retryDraft(), nil)
	ctx := context.Background()

	w.processAgentic(ctx, task, project, profile)
	if sb.executionCount != 1 {
		t.Fatalf("first attempt ran %d commands, want 1", sb.executionCount)
	}
	w.processAgentic(ctx, task, project, profile)

	if sb.executionCount != 1 {
		t.Fatalf("retry ran the failing command again (executions = %d), want it refused", sb.executionCount)
	}
	if len(gw.requests) < 4 || !requestContainsToolResult(gw.requests[3], "c2", "--- FAIL: TestCountLines") {
		t.Fatal("the refused retry did not carry the previous failure back to the model")
	}
}

// A retry that sends a different command runs normally: only the identical
// command is refused.
func TestAgenticRetryRunsDifferentCommand(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "make test"), {Content: "Done."},
		bashCall("c2", "make build"), {Content: "Done."},
	}}
	sb := &mockAgenticSandbox{results: map[string]sandbox.Result{
		"make test": {Success: false, ExitCode: 1, Stderr: "--- FAIL: TestCountLines"},
	}}
	w, _, task, project, profile := agenticFixture(t, gw, sb, retryDraft(), nil)
	ctx := context.Background()

	w.processAgentic(ctx, task, project, profile)
	w.processAgentic(ctx, task, project, profile)

	if sb.executionCount != 2 || !strings.HasPrefix(sb.lastCommand, "make build") {
		t.Fatalf("executions = %d, last command = %q; want the new command to run on the retry", sb.executionCount, sb.lastCommand)
	}
}
