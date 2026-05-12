package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

func TestTruncateToMaxMultibyteInput(t *testing.T) {
	input := strings.Repeat("界", maxOutputSummaryLength)
	if got := truncateToMax(input, maxOutputSummaryLength); got != input {
		t.Fatalf("truncateToMax changed input at max length: got %d runes, want %d", utf8.RuneCountInString(got), maxOutputSummaryLength)
	}

	overLimit := input + "界"
	got := truncateToMax(overLimit, maxOutputSummaryLength)
	if gotLen := utf8.RuneCountInString(got); gotLen != maxOutputSummaryLength {
		t.Fatalf("truncateToMax length = %d runes, want %d", gotLen, maxOutputSummaryLength)
	}
	if !strings.HasSuffix(got, truncationSuffix) {
		t.Fatalf("truncateToMax result should include truncation suffix")
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncateToMax result should be valid UTF-8")
	}
}

func TestEmitToolResultValidJSONWithoutSuccessIsSuccessfulOutput(t *testing.T) {
	sink := &mockEventSink{}
	w := &Worker{sink: sink}
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-test-123"},
		ProjectID:  "proj-test-456",
	}
	call := gateway.ToolCall{
		ID:       "call-json-output",
		Function: gateway.ToolCallFunction{Name: "read"},
	}
	result := `{"answer":42}`

	w.emitToolResult(context.Background(), task, call, result, 25)

	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}

	var event ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[0].Payload), &event); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if event.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", event.ExitCode)
	}
	if event.OutputSummary != result {
		t.Fatalf("OutputSummary = %q, want %q", event.OutputSummary, result)
	}
	if event.StdoutBytes != len(result) {
		t.Fatalf("StdoutBytes = %d, want %d", event.StdoutBytes, len(result))
	}
	if event.StderrBytes != 0 {
		t.Fatalf("StderrBytes = %d, want 0", event.StderrBytes)
	}
}

func TestEmitToolResultExplicitJSONFailures(t *testing.T) {
	tests := []struct {
		name   string
		result string
	}{
		{name: "error", result: `{"error":"boom"}`},
		{name: "fatal", result: `{"FatalError":"boom"}`},
		{name: "success false", result: `{"Success":false,"ExitCode":1}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &mockEventSink{}
			w := &Worker{sink: sink}
			task := models.Task{
				BaseEntity: models.BaseEntity{ID: "task-test-123"},
				ProjectID:  "proj-test-456",
			}
			call := gateway.ToolCall{
				ID:       "call-json-failure",
				Function: gateway.ToolCallFunction{Name: "bash"},
			}

			w.emitToolResult(context.Background(), task, call, tc.result, 25)

			if len(sink.events) != 1 {
				t.Fatalf("events = %d, want 1", len(sink.events))
			}

			var event ToolResultEvent
			if err := json.Unmarshal([]byte(sink.events[0].Payload), &event); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if event.ExitCode != -1 {
				t.Fatalf("ExitCode = %d, want -1", event.ExitCode)
			}
		})
	}
}

func TestEmitToolResultJSONEnvelopeByteCounts(t *testing.T) {
	sink := &mockEventSink{}
	w := &Worker{sink: sink}
	task := models.Task{
		BaseEntity: models.BaseEntity{ID: "task-test-123"},
		ProjectID:  "proj-test-456",
	}
	call := gateway.ToolCall{
		ID:       "call-json-envelope",
		Function: gateway.ToolCallFunction{Name: "bash"},
	}
	result := `{"Success":true,"ExitCode":0,"Stdout":"hello","Stderr":"warn"}`

	w.emitToolResult(context.Background(), task, call, result, 25)

	if len(sink.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sink.events))
	}

	var event ToolResultEvent
	if err := json.Unmarshal([]byte(sink.events[0].Payload), &event); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if event.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", event.ExitCode)
	}
	if event.StdoutBytes != len("hello") {
		t.Fatalf("StdoutBytes = %d, want %d", event.StdoutBytes, len("hello"))
	}
	if event.StderrBytes != len("warn") {
		t.Fatalf("StderrBytes = %d, want %d", event.StderrBytes, len("warn"))
	}
}
