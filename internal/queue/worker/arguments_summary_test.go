package worker

import (
	"context"
	"math/rand"
	"testing"
	"unicode/utf8"

	"agentd/internal/models"
)

// TestArgumentsSummaryLengthBound tests Property 3: Arguments Summary Length Bound.
// Validates: Requirements 1.4
//
// For any TOOL_CALL event, the arguments_summary field SHALL NOT exceed 200 characters.
//
// This property test runs 150 iterations (> 100 as required) with random arguments
// of varying lengths to ensure the truncation always works correctly.
func TestArgumentsSummaryLengthBound(t *testing.T) {
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

		for i := range seq.Calls {
			seq.Calls[i].Function.Arguments = generateRandomArguments(rnd)
		}

		for _, call := range seq.Calls {
			w.emitToolCall(ctx, task, call)
		}

		for _, ev := range sink.events {
			if ev.Type != models.EventTypeToolCall {
				continue
			}

			argsSummaryResult := extractArgumentsSummary(ev.Payload)
			if argsSummaryResult.err != nil {
				t.Logf("Failed to decode TOOL_CALL payload: %v", argsSummaryResult.err)
				t.Logf("Payload: %s", ev.Payload)
				return false
			}
			argsSummaryLength := utf8.RuneCountInString(argsSummaryResult.value)
			if argsSummaryLength > maxArgumentsSummaryLength {
				t.Logf("arguments_summary length: %d (max: %d)", argsSummaryLength, maxArgumentsSummaryLength)
				t.Logf("Payload: %s", ev.Payload)
				return false
			}
		}

		if len(sink.events) != len(seq.Calls) {
			return false
		}

		return true
	}

	runPropertyTest(t, "ArgumentsSummaryLengthBound", iterations, property)
}
