package worker

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"agentd/internal/models"
)

// TestEventOrderingWithVaryingCallCounts tests event ordering with different
// numbers of tool calls to ensure the property holds regardless of sequence length.
func TestEventOrderingWithVaryingCallCounts(t *testing.T) {
	testCounts := []int{1, 2, 3, 5, 10, 15, 20}

	for _, count := range testCounts {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			seed := time.Now().UnixNano()
			rnd := rand.New(rand.NewSource(seed))
			t.Logf("seed=%d count=%d", seed, count)

			seq := generateToolCallSequence(rnd, count)
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
				w.emitToolResult(ctx, task, call, SuccessResult(call.ID, `{"Success":true}`, 100))
			}

			callPositions := make(map[string]int)
			for i, ev := range sink.events {
				switch ev.Type {
				case models.EventTypeToolCall:
					callID := extractCallID(ev.Payload)
					callPositions[callID] = i
				case models.EventTypeToolResult:
					callID := extractCallID(ev.Payload)
					pos, ok := callPositions[callID]
					if !ok {
						t.Errorf("No TOOL_CALL found for call_id %s", callID)
						return
					}
					if i <= pos {
						t.Errorf("TOOL_RESULT at index %d should be after TOOL_CALL at index %d for call_id %s",
							i, pos, callID)
						return
					}
				}
			}

			if len(sink.events) != count*2 {
				t.Errorf("Expected %d events, got %d", count*2, len(sink.events))
			}
		})
	}
}
