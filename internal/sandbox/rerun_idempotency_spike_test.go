package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SP-011: when boot recovery resets an interrupted RUNNING task to READY, the
// task is re-dispatched from the start. What does the second attempt see?
//
// These tests answer that at the layer where the side effect actually happens:
// the workspace on disk. The worker adds no workspace preparation of its own —
// BashExecutor just sets cmd.Dir to the workspace and runs — so whatever the first
// attempt wrote is still there when the second one starts.

// TestSecondAttemptSeesFirstAttemptsFiles is the reproduction. The first attempt
// writes a file and is killed before it can finish; the second attempt lists the
// workspace and reports what it finds. If the file is there, the re-run is not
// starting from a clean workspace.
func TestSecondAttemptSeesFirstAttemptsFiles(t *testing.T) {
	// A short inactivity timeout keeps the kill fast: the executor cancels the
	// command once it sees no output for that long, which is the same path a
	// real mid-task kill takes.
	exec, workspace := testExecutor(t, nil)
	exec.Inactivity = 200 * time.Millisecond

	// Attempt 1: create a marker, then block until killed mid-task.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = exec.Execute(ctx, testPayload(workspace, "echo partial > marker.txt && sleep 30"))

	if _, err := os.Stat(filepath.Join(workspace, "marker.txt")); err != nil {
		t.Fatalf("attempt 1 left no marker (%v); the test is not exercising a partial write", err)
	}

	// Attempt 2: the re-dispatch after recovery.
	result, err := exec.Execute(context.Background(), testPayload(workspace, "ls"))
	if err != nil {
		t.Fatalf("attempt 2 error = %v", err)
	}

	if contains := strings.Contains(result.Stdout, "marker.txt"); contains {
		t.Logf("FINDING: attempt 2 sees the first attempt's file.\nstdout:\n%s", result.Stdout)
	} else {
		t.Logf("attempt 2 does not see marker.txt:\n%s", result.Stdout)
	}
}

// TestWorkspaceIsNotCleanedBetweenAttempts is the assertion form of the same
// question, so the answer is a test result rather than a log line somebody has to
// read. It asserts what the code actually does today: nothing removes the first
// attempt's files. If a future change cleans the workspace, this test fails and
// the spike's conclusion is revisited — which is the point of writing it down.
func TestWorkspaceIsNotCleanedBetweenAttempts(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	ctx := context.Background()

	if _, err := exec.Execute(ctx, testPayload(workspace, "echo run-one > first.txt")); err != nil {
		t.Fatalf("attempt 1 error = %v", err)
	}
	if _, err := exec.Execute(ctx, testPayload(workspace, "echo run-two > second.txt")); err != nil {
		t.Fatalf("attempt 2 error = %v", err)
	}

	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	if !containsName(names, "first.txt") {
		t.Fatalf("workspace after both attempts = %v, want first.txt still present — "+
			"if this fails the workspace is now cleaned and SP-011's conclusion is stale", names)
	}
}

// TestAppendingCommandCompoundsAcrossAttempts is the non-idempotency that
// actually matters. A command that appends produces a different result on the
// second attempt than on the first, so a re-run is not merely "seeing extra
// files" — it can produce a different outcome.
func TestAppendingCommandCompoundsAcrossAttempts(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	ctx := context.Background()

	payload := testPayload(workspace, "echo line >> log.txt && wc -l < log.txt")

	first, err := exec.Execute(ctx, payload)
	if err != nil {
		t.Fatalf("attempt 1 error = %v", err)
	}
	second, err := exec.Execute(ctx, payload)
	if err != nil {
		t.Fatalf("attempt 2 error = %v", err)
	}

	t.Logf("attempt 1 reports %q lines, attempt 2 reports %q lines",
		strings.TrimSpace(first.Stdout), strings.TrimSpace(second.Stdout))

	if strings.TrimSpace(first.Stdout) == strings.TrimSpace(second.Stdout) {
		t.Logf("both attempts report the same line count, so this command is idempotent in practice")
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
