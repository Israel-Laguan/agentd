package worker

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/models"
	"agentd/internal/sandbox"
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
