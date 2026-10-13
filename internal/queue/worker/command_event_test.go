package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
	"agentd/internal/testutil"
)

type fixedResultSandbox struct {
	result sandbox.Result
	err    error
}

func (s fixedResultSandbox) Execute(context.Context, sandbox.Payload) (sandbox.Result, error) {
	return s.result, s.err
}

func commandEvents(sink *mockEventSink) []string {
	var got []string
	for _, ev := range sink.events {
		if ev.Type == models.EventTypeCommand {
			got = append(got, ev.Payload)
		}
	}
	return got
}

func runLegacyCommand(t *testing.T, sb sandbox.Executor, command string) *mockEventSink {
	t.Helper()
	ctx := context.Background()
	store := testutil.NewFakeStore()
	task := materializeLegacyReviewTask(t, store, ctx, "cmd-event", "inspect host")
	project, err := store.GetProject(ctx, task.ProjectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	sink := &mockEventSink{}
	w := NewWorker(store, &routingTestGateway{}, sb, nil, sink, WorkerOptions{})
	w.executeLegacyCommand(ctx, task, *project, models.AgentProfile{}, command, &legacyTaskAudit{})
	return sink
}

// An operator must be able to see which command produced a task's output.
func TestExecuteLegacyCommandRecordsExecutedCommand(t *testing.T) {
	t.Parallel()
	sink := runLegacyCommand(t, fixedResultSandbox{result: sandbox.Result{Success: true, Stdout: "ok"}}, "uname -a > os.txt")
	if got := commandEvents(sink); len(got) != 1 || got[0] != "uname -a > os.txt" {
		t.Fatalf("COMMAND events = %#v, want the executed command", got)
	}
}

func TestExecuteLegacyCommandDoesNotRecordRefusedCommand(t *testing.T) {
	t.Parallel()
	refused := fmt.Errorf("%w: absolute path escapes workspace", models.ErrSandboxViolation)
	sink := runLegacyCommand(t, fixedResultSandbox{result: sandbox.Result{ExitCode: -1}, err: refused}, "cat /etc/os-release")
	if got := commandEvents(sink); len(got) != 0 {
		t.Fatalf("COMMAND events = %#v, want none for a refused command", got)
	}
}

type redactingScrubber struct{}

func (redactingScrubber) Scrub(s string) string {
	return strings.ReplaceAll(s, "sk-secret", "[REDACTED]")
}

func TestExecutedCommandPayloadIsScrubbedAndBounded(t *testing.T) {
	t.Parallel()
	w := &Worker{sandboxScrubber: redactingScrubber{}}
	if got := w.executedCommandPayload("curl -H sk-secret host"); got != "curl -H [REDACTED] host" {
		t.Fatalf("payload = %q, want secret redacted", got)
	}
	if got := w.executedCommandPayload(strings.Repeat("x", 5000)); len([]rune(got)) > 1100 {
		t.Fatalf("payload length = %d, want bounded", len([]rune(got)))
	}
}
