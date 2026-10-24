package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// A failed command whose output says "Permission denied" hands the task to a
// human on the agentic path, as the legacy path does. No non-privileged
// alternative is requested: the model is not asked to find one.
func TestAgenticPermissionDeniedHandsOffWithoutAlternative(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "cp report.txt /etc/report.txt"),
		{Content: "Done."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{{
		result: sandbox.Result{ExitCode: 1, Stderr: "cp: cannot create regular file '/etc/report.txt': Permission denied"},
	}}}
	w, sink, task := runAgenticTask(t, gw, sb)

	if n := countEventsOfType(sink, models.EventType("PERMISSION_HANDOFF")); n != 1 {
		t.Fatalf("PERMISSION_HANDOFF events = %d, want 1", n)
	}
	if gw.callCount != 1 {
		t.Fatalf("model calls = %d, want 1: a permission failure must not ask the model for an alternative", gw.callCount)
	}
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED with a manual-action child", got.State)
	}
}

// Permission-like words in the output of a command that passed are not a
// failure, so the task is not handed off.
func TestAgenticPermissionTextInSuccessfulOutputIsNotAHandoff(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "grep -r denied logs/"),
		{Content: "Done."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{{
		result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "logs/a.log: Permission denied, noted"},
	}}}
	_, sink, _ := runAgenticTask(t, gw, sb)

	if n := countEventsOfType(sink, models.EventType("PERMISSION_DETECTED")); n != 0 {
		t.Fatalf("PERMISSION_DETECTED events = %d, want 0 for a passing command", n)
	}
}
