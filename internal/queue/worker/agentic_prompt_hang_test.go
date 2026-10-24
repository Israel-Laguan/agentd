package worker

import (
	"context"
	"fmt"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
)

// promptHangStep is a command that stopped for lack of output at an
// interactive prompt: the sandbox timed it out and kept what it printed.
func promptHangStep() scriptedStep {
	return scriptedStep{
		result: sandbox.Result{TimedOut: true, Stdout: "Do you want to continue? [y/N] "},
		err:    fmt.Errorf("%w: no output within limit", models.ErrExecutionTimeout),
	}
}

func countEventsOfType(sink *mockEventSink, typ models.EventType) int {
	n := 0
	for _, ev := range sink.events {
		if ev.Type == typ {
			n++
		}
	}
	return n
}

// A command that hangs at a prompt gets corrective feedback the first time, so
// the model can rerun it non-interactively. Nothing is handed off.
func TestAgenticPromptHangFirstTimeGivesFeedbackNotHandoff(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "apt-get install nginx"),
		bashCall("c2", "apt-get -y install nginx"),
		{Content: "Done. nginx is installed."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		promptHangStep(),
		{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "installed"}},
	}}
	w, sink, task := runAgenticTask(t, gw, sb)

	if n := countEventsOfType(sink, models.EventType("PROMPT_DETECTED")); n != 1 {
		t.Fatalf("PROMPT_DETECTED events = %d, want 1", n)
	}
	if n := countEventsOfType(sink, models.EventType("PROMPT_HANDOFF")); n != 0 {
		t.Fatalf("PROMPT_HANDOFF events = %d, want 0 on the first hang", n)
	}
	if len(gw.requests) < 2 || !requestContainsToolResult(gw.requests[1], "c1", "-y") {
		t.Fatal("the model did not get corrective feedback naming a non-interactive rerun")
	}
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED after the non-interactive rerun passed", got.State)
	}
}

// The same command hanging again is a repeat: the task goes to a human.
func TestAgenticPromptHangRepeatedCommandHandsOff(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "apt-get install nginx"),
		bashCall("c2", "apt-get install nginx"),
		{Content: "Done."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{promptHangStep(), promptHangStep()}}
	w, sink, task := runAgenticTask(t, gw, sb)

	if n := countEventsOfType(sink, models.EventType("PROMPT_HANDOFF")); n != 1 {
		t.Fatalf("PROMPT_HANDOFF events = %d, want 1 when the same command hangs twice", n)
	}
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateBlocked {
		t.Fatalf("state = %s, want BLOCKED with a manual-action child", got.State)
	}
}

// A timeout whose output shows no prompt is an ordinary failure, not a hang.
func TestAgenticTimeoutWithoutPromptIsNotAPromptHang(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "make build"),
		{Content: "Done."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{{
		result: sandbox.Result{TimedOut: true, Stdout: "compiling"},
		err:    fmt.Errorf("%w: no output within limit", models.ErrExecutionTimeout),
	}}}
	_, sink, _ := runAgenticTask(t, gw, sb)

	if n := countEventsOfType(sink, models.EventType("PROMPT_DETECTED")); n != 0 {
		t.Fatalf("PROMPT_DETECTED events = %d, want 0 for a timeout with no prompt", n)
	}
}
