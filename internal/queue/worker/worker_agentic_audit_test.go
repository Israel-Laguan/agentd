package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/config"
	"agentd/internal/gateway"
	"agentd/internal/sandbox"
)

// withAuditFile turns on the structured audit log for w, written to a file in
// the test's temp dir, and returns the path.
func withAuditFile(t *testing.T, w *Worker) string {
	t.Helper()
	path := filepathJoinTemp(t, "audit.jsonl")
	w.auditLogger = newAuditLogger(config.AuditConfig{Enabled: true, Path: path})
	return path
}

// auditTypesFor returns the task lifecycle record types written for taskID, in
// order. Tool and turn records for the same task are left out.
func auditTypesFor(t *testing.T, path, taskID string) []string {
	t.Helper()
	var types []string
	for _, rec := range readAuditRecords(t, path) {
		kind, _ := rec["record_type"].(string)
		if rec["task_id"] != taskID || !strings.HasPrefix(kind, "task_") {
			continue
		}
		types = append(types, kind)
	}
	return types
}

// An agentic attempt writes a task_start on entry and a task_complete when its
// loop ends in success, as the legacy path does.
func TestAgenticAttemptAuditsStartAndComplete(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{{Content: "Done."}}}
	w, _, task, project, profile := agenticFixture(t, gw, &scriptedSandbox{}, retryDraft(), nil)
	path := withAuditFile(t, w)

	w.processAgentic(context.Background(), task, project, profile)

	got := auditTypesFor(t, path, task.ID)
	if len(got) != 2 || got[0] != "task_start" || got[1] != "task_complete" {
		t.Fatalf("task audit records = %v, want [task_start task_complete]", got)
	}
}

// A loop that ends without a passing result writes a task_fail, not a
// task_complete.
func TestAgenticAttemptAuditsFailWhenLoopDoesNotPass(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		bashCall("c1", "make test"), {Content: "Done."},
	}}
	sb := &scriptedSandbox{steps: []scriptedStep{
		{result: sandbox.Result{ExitCode: 1, Stderr: "--- FAIL: TestCountLines"}},
	}}
	w, _, task, project, profile := agenticFixture(t, gw, sb, retryDraft(), nil)
	path := withAuditFile(t, w)

	w.processAgentic(context.Background(), task, project, profile)

	got := auditTypesFor(t, path, task.ID)
	if len(got) != 2 || got[0] != "task_start" || got[1] != "task_fail" {
		t.Fatalf("task audit records = %v, want [task_start task_fail]", got)
	}
}
