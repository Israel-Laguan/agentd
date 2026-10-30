package worker

import (
	"context"
	"testing"

	agenthooks "agentd/internal/agent/hooks"
	agenttools "agentd/internal/agent/tools"
	"agentd/internal/gateway"
	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

// A failed command whose output says "Permission denied" hands the task to a
// human. No non-privileged alternative is requested: the model is not asked to
// find one.
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

// The always-on denylist refuses sudo before the sandbox sees it, so in
// production the sandbox's "sudo command blocked" output never appears. The
// denylist's veto is the sudo refusal, and it is a privilege block too.
func TestAgenticPrivilegeBlockDetectsDenylistSudoVeto(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	call := bashCall("c1", "sudo dmidecode -t memory").ToolCalls[0]
	veto := agenttools.VetoedResult("c1", agenthooks.DenylistSudoReason)

	command, blocked := w.AgenticPrivilegeBlock(call, veto)
	if !blocked || command != "sudo dmidecode -t memory" {
		t.Fatalf("AgenticPrivilegeBlock = (%q, %v), want the sudo command blocked: the denylist refused it", command, blocked)
	}
}

// The denylist refuses other dangerous commands too. Those are not privilege
// failures, so they get no alternative and no handoff.
func TestAgenticPrivilegeBlockIgnoresOtherDenylistVetoes(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	call := bashCall("c1", "rm -rf /").ToolCalls[0]
	veto := agenttools.VetoedResult("c1", `command blocked: contains dangerous pattern "rm -rf /"`)

	if _, blocked := w.AgenticPrivilegeBlock(call, veto); blocked {
		t.Fatalf("AgenticPrivilegeBlock blocked a rm -rf / veto; only sudo is a privilege block")
	}
}

// The J19 path, end to end: a sudo command vetoed by the denylist gets one
// non-privileged alternative from the model, which runs, and the task completes.
func TestAgenticDenylistSudoVetoRecoversWithAlternative(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "sudo dmidecode -t memory"),
		{Content: `{"command":"free -h"}`},
		{Content: "Memory reported."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{{result: sandbox.Result{Success: true, ExitCode: 0, Stdout: "Mem: 16G"}}}}
	w, _, task, project, profile := agenticFixture(t, gw, sb, models.DraftTask{Title: "Inspect host", Description: testutil.AgenticTestTaskDescription()}, nil)
	w.hooks = agenthooks.NewHookChain()
	w.hooks.RegisterPre(agenthooks.DenylistHook(t.TempDir()))

	if result, ok := w.processAgentic(context.Background(), task, project, profile); ok {
		w.handleLoopResult(context.Background(), task, result)
	}
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED after the non-privileged alternative ran", got.State)
	}
	if len(sb.steps) != 0 {
		t.Fatalf("sandbox steps left = %d, want the alternative to have run once", len(sb.steps))
	}
}
