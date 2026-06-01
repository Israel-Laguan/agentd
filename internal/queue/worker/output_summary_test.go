package worker

import (
	"context"
	"math/rand"
	"testing"
	"unicode/utf8"

	agenttools "agentd/internal/agent/tools"
	"agentd/internal/models"
)

// TestOutputSummaryLengthBound tests Property 4: Output Summary Length Bound.
// Validates: Requirements 4.1
//
// For any TOOL_RESULT event, the output_summary field SHALL NOT exceed 1000 characters
// (including "...[truncated]" suffix when applicable).
//
// This property test runs 150 iterations (> 100 as required) with random tool outputs
// of varying lengths to ensure the truncation always works correctly.
func TestOutputSummaryLengthBound(t *testing.T) {
	iterations := 150

	property := func(seq ToolCallSequence, rnd *rand.Rand) bool {
		sink := &mockEventSink{}
		w := &Worker{
			sink:            sink,
			sandboxScrubber: nil,
		}

		task := models.Task{
			BaseEntity: models.BaseEntity{ID: "task-test-123"},
			ProjectID:  "proj-test-456",
		}

		ctx := context.Background()

		for _, call := range seq.Calls {
			output := generateRandomOutput(rnd)
			w.emitToolResult(ctx, task, call, agenttools.SuccessResult(call.ID, output, 100))
		}

		for _, ev := range sink.events {
			if ev.Type != models.EventTypeToolResult {
				continue
			}

			outputSummaryResult := extractOutputSummary(ev.Payload)
			if outputSummaryResult.err != nil {
				t.Logf("Failed to decode TOOL_RESULT payload: %v", outputSummaryResult.err)
				t.Logf("Payload: %s", ev.Payload)
				return false
			}
			outputSummaryLength := utf8.RuneCountInString(outputSummaryResult.value)
			if outputSummaryLength > maxOutputSummaryLength {
				t.Logf("output_summary length: %d (max: %d)", outputSummaryLength, maxOutputSummaryLength)
				t.Logf("Payload: %s", ev.Payload)
				return false
			}
		}

		return len(sink.events) == len(seq.Calls)
	}

	runPropertyTest(t, "OutputSummaryLengthBound", iterations, property)
}
