package worker

import (
	"context"
	"math/rand"
	"testing"

	"agentd/internal/models"
)

// TestToolCallPrecedesToolResult tests Property 1: Tool Call Precedes Tool Result.
// Validates: Requirements 6.1, 6.3
//
// For any tool execution in the agentic loop, the TOOL_CALL event SHALL be emitted
// before the corresponding TOOL_RESULT event with matching call_id.
//
// This property test runs 150 iterations (> 100 as required) with random tool call sequences.
func TestToolCallPrecedesToolResult(t *testing.T) {
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
			w.emitToolCall(ctx, task, call)
			w.emitToolResult(ctx, task, call, `{"Success":true}`, 100)
		}

		callPositions := make(map[string]int)

		for i, ev := range sink.events {
			switch ev.Type {
			case models.EventTypeToolCall:
				callID := extractCallID(ev.Payload)
				callPositions[callID] = i
			case models.EventTypeToolResult:
				callID := extractCallID(ev.Payload)
				if pos, ok := callPositions[callID]; !ok {
					return false
				} else if i <= pos {
					return false
				}
			}
		}

		expectedEvents := len(seq.Calls) * 2
		if len(sink.events) != expectedEvents {
			return false
		}

		toolCallCount := 0
		toolResultCount := 0
		for _, ev := range sink.events {
			switch ev.Type {
			case models.EventTypeToolCall:
				toolCallCount++
			case models.EventTypeToolResult:
				toolResultCount++
			}
		}

		if toolCallCount != len(seq.Calls) || toolResultCount != len(seq.Calls) {
			return false
		}

		return true
	}

	runPropertyTest(t, "ToolCallPrecedesToolResult", iterations, property)
}

// TestToolCallIDMatching tests Property 2: Call ID Matching.
// Validates: Requirements 6.3
//
// The call_id in the TOOL_CALL event SHALL exactly match the call_id
// in the corresponding TOOL_RESULT event.
func TestToolCallIDMatching(t *testing.T) {
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
			w.emitToolCall(ctx, task, call)
			w.emitToolResult(ctx, task, call, `{"Success":true}`, 100)
		}

		pending := make(map[string]int)

		for _, ev := range sink.events {
			callID := extractCallID(ev.Payload)
			if callID == "" {
				return false
			}

			switch ev.Type {
			case models.EventTypeToolCall:
				pending[callID]++
			case models.EventTypeToolResult:
				if pending[callID] == 0 {
					return false
				}
				pending[callID]--
			}
		}

		for _, unmatched := range pending {
			if unmatched != 0 {
				return false
			}
		}

		return true
	}

	runPropertyTest(t, "ToolCallIDMatching", iterations, property)
}
