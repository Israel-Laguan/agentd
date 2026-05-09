package sandbox

import (
	"agentd/internal/bus"
	"agentd/internal/models"
	"agentd/internal/testutil"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBashExecutorCapturesStdout(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	result, err := exec.Execute(context.Background(), testPayload(workspace, "echo hello"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.ExitCode != 0 || !result.Success || result.Stdout != "hello\n" {
		t.Fatalf("result = %#v", result)
	}
	if len(sink.events) != 1 || sink.events[0].Type != "LOG_CHUNK" || sink.events[0].Payload != "hello" {
		t.Fatalf("events = %#v", sink.events)
	}
}

func TestBashExecutorExitCode(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	result, err := exec.Execute(context.Background(), testPayload(workspace, "exit 7"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
}

func TestBashExecutorInactivityTimeout(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	exec.Inactivity = 100 * time.Millisecond
	result, err := exec.Execute(context.Background(), testPayload(workspace, "sleep 5"))
	if !errors.Is(err, models.ErrExecutionTimeout) {
		t.Fatalf("Execute() error = %v, want ErrExecutionTimeout", err)
	}
	if !result.TimedOut {
		t.Fatalf("TimedOut = false")
	}
}

func TestBashExecutorWallTimeoutKillsProcessGroup(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	exec.KillGrace = 100 * time.Millisecond
	payload := testPayload(workspace, "sleep 300")
	payload.TimeoutLimit = 1
	started := time.Now()
	result, err := exec.Execute(context.Background(), payload)
	if !errors.Is(err, models.ErrExecutionTimeout) {
		t.Fatalf("Execute() error = %v, want ErrExecutionTimeout", err)
	}
	if !result.TimedOut || result.Success {
		t.Fatalf("result = %#v", result)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatalf("timeout took %s, want close to 1s", time.Since(started))
	}
	assertNoSleep300(t)
}

func TestBashExecutorRejectsWorkspaceEscape(t *testing.T) {
	exec, _ := testExecutor(t, nil)
	payload := testPayload(t.TempDir(), "echo no")
	payload.WorkspacePath = t.TempDir()
	_, err := exec.Execute(context.Background(), payload)
	if !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("Execute() error = %v, want ErrSandboxViolation", err)
	}
}

func TestBashExecutorBlocksDirectoryTraversalCommand(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	result, err := exec.Execute(context.Background(), testPayload(workspace, "cat ../../../etc/passwd"))
	if !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("Execute() error = %v, want ErrSandboxViolation", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = %d, want non-zero", result.ExitCode)
	}
	if len(sink.events) != 1 || sink.events[0].Type != "SANDBOX_VIOLATION" {
		t.Fatalf("events = %#v", sink.events)
	}
}

func TestBashExecutorRejectsAbsolutePathEscape(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	_, err := exec.Execute(context.Background(), testPayload(workspace, "cat /etc/passwd"))
	if !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("Execute() error = %v, want ErrSandboxViolation", err)
	}
}

func TestBashExecutorAllowsWorkspaceRelativeCommand(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	dir := filepath.Join(workspace, "sub")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	result, err := exec.Execute(context.Background(), testPayload(workspace, "cd ./sub && pwd"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Stdout, filepath.Clean(dir)) {
		t.Fatalf("Stdout = %q, want to contain %q", result.Stdout, dir)
	}
}

func TestBashExecutorRejectsHomePathReference(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	_, err := exec.Execute(context.Background(), testPayload(workspace, "cat $HOME/.ssh/id_rsa"))
	if !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("Execute() error = %v, want ErrSandboxViolation", err)
	}
}

func TestBashExecutorBlocksSudoCommand(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	result, err := exec.Execute(context.Background(), testPayload(workspace, "echo ok && sudo touch /etc/agentd.conf"))
	if !errors.Is(err, models.ErrSandboxViolation) {
		t.Fatalf("Execute() error = %v, want ErrSandboxViolation", err)
	}
	if result.ExitCode == 0 {
		t.Fatalf("ExitCode = %d, want non-zero", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "sudo command blocked") {
		t.Fatalf("Stderr = %q", result.Stderr)
	}
	if len(sink.events) != 1 || sink.events[0].Type != "SANDBOX_VIOLATION" {
		t.Fatalf("events = %#v", sink.events)
	}
}

func TestBashExecutorTruncatesLargeLogs(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	exec.MaxLogBytes = 1024
	exec.Inactivity = 5 * time.Second
	result, err := exec.Execute(context.Background(), testPayload(workspace, "for i in $(seq 1 3000); do echo hello; done"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(result.Stdout, "bytes truncated") {
		t.Fatalf("Stdout should contain truncation marker, got %q", result.Stdout)
	}
	if len(result.Stdout) > 1800 {
		t.Fatalf("len(Stdout) = %d, want <= 1800", len(result.Stdout))
	}
}

func TestBashExecutorScrubsLogs(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	exec.Scrubber = NewScrubber([]string{`custom-secret-[A-Za-z0-9]+`})
	result, err := exec.Execute(
		context.Background(),
		testPayload(workspace, `echo "sk-1234567890123456789012345678901234567890 custom-secret-abc123"`),
	)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Contains(result.Stdout, "sk-123456") || strings.Contains(result.Stdout, "custom-secret-") {
		t.Fatalf("Stdout leaked secret: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "[REDACTED]") {
		t.Fatalf("Stdout = %q, want [REDACTED]", result.Stdout)
	}
}

func TestBashExecutorSigtermGrace(t *testing.T) {
	exec, workspace := testExecutor(t, nil)
	exec.KillGrace = 200 * time.Millisecond
	payload := testPayload(workspace, `trap "echo caught; exit 0" TERM; sleep 300`)
	payload.WallTimeout = 100 * time.Millisecond
	result, err := exec.Execute(context.Background(), payload)
	if !errors.Is(err, models.ErrExecutionTimeout) {
		t.Fatalf("Execute() error = %v, want ErrExecutionTimeout", err)
	}
	if !result.TimedOut {
		t.Fatalf("result.TimedOut = false")
	}
}

func TestBashExecutorCapturesStderr(t *testing.T) {
	sink := &recordingSink{}
	exec, workspace := testExecutor(t, sink)
	result, err := exec.Execute(context.Background(), testPayload(workspace, "echo bad >&2"))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Stderr != "bad\n" || len(sink.events) != 1 || sink.events[0].Type != "LOG_CHUNK" {
		t.Fatalf("result=%#v events=%#v", result, sink.events)
	}
}

func TestBashExecutorPersistsAndStreamsLogChunks(t *testing.T) {
	ctx := context.Background()
	store := testutil.NewFakeStore()
	project, tasks := materializeSandboxTask(t, store)
	eventBus := bus.NewInProcess()
	ch, unsubscribe := eventBus.Subscribe("project:"+project.ID, 3)
	defer unsubscribe()
	emitter := &persistingBusSink{store: store, bus: eventBus}

	exec, payload := streamingExecutorPayload(t, ctx, project.ID, tasks[0].ID, emitter)

	done := make(chan error, 1)
	go func() {
		_, err := exec.Execute(ctx, payload)
		done <- err
	}()

	first := receiveEvent(t, ch, 500*time.Millisecond)
	if first.Payload != "one" || first.Type != "LOG_CHUNK" {
		t.Fatalf("first event = %#v", first)
	}
	second := receiveEvent(t, ch, 1500*time.Millisecond)
	third := receiveEvent(t, ch, 1500*time.Millisecond)
	if second.Payload != "two" || third.Payload != "three" {
		t.Fatalf("events = %#v %#v", second, third)
	}
	if err := <-done; err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	assertEventCount(t, store, 3)
}

func streamingExecutorPayload(
	t *testing.T,
	ctx context.Context,
	projectID string,
	taskID string,
	emitter models.EventSink,
) (*BashExecutor, Payload) {
	t.Helper()
	root := t.TempDir()
	workspace, err := (&FSWorkspaceManager{Root: root}).EnsureProjectDir(ctx, projectID)
	if err != nil {
		t.Fatalf("EnsureProjectDir() error = %v", err)
	}
	payload := Payload{
		ProjectID: projectID, TaskID: taskID, WorkspacePath: workspace,
		Command: `printf 'one\n'; sleep 1; printf 'two\n'; sleep 1; printf 'three\n'`, TimeoutLimit: 5,
	}
	return &BashExecutor{Root: root, Sink: emitter, Inactivity: 5 * time.Second}, payload
}

func testExecutor(t *testing.T, sink models.EventSink) (*BashExecutor, string) {
	t.Helper()
	root := t.TempDir()
	workspace, err := (&FSWorkspaceManager{Root: root}).EnsureProjectDir(context.Background(), "p")
	if err != nil {
		t.Fatalf("EnsureProjectDir() error = %v", err)
	}
	return &BashExecutor{Root: root, Sink: sink, Inactivity: time.Second}, workspace
}

func testPayload(workspace, command string) Payload {
	return Payload{ProjectID: "p", TaskID: "t", WorkspacePath: filepath.Clean(workspace), Command: command}
}
