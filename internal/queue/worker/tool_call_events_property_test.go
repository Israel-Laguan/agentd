package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// mockEventSink records emitted events for testing.
type mockEventSink struct {
	events []models.Event
}

func (m *mockEventSink) Emit(ctx context.Context, ev models.Event) error {
	m.events = append(m.events, ev)
	return nil
}

// ToolCallSequence represents a sequence of tool calls for property testing.
type ToolCallSequence struct {
	Calls []gateway.ToolCall
}

// generateToolCallSequence generates a random sequence of tool calls.
func generateToolCallSequence(rnd *rand.Rand, size int) ToolCallSequence {
	if size < 1 {
		size = 1
	}
	if size > 20 {
		size = 20 // Cap at 20 for reasonable test times
	}

	toolNames := []string{"bash", "read", "write"}
	commands := []string{
		`{"command":"echo hello"}`,
		`{"command":"ls -la"}`,
		`{"path":"file.txt"}`,
		`{"path":"file.txt","content":"hello world"}`,
		`{"command":"cat /etc/passwd"}`,
		`{"path":"/tmp/test.txt"}`,
		`{"command":"pwd"}`,
		`{"path":"config.yaml","content":"key: value"}`,
		`{"command":"whoami"}`,
		`{"path":"data.json"}`,
	}

	calls := make([]gateway.ToolCall, size)
	for i := 0; i < size; i++ {
		calls[i] = gateway.ToolCall{
			ID: fmt.Sprintf("call_%d_%d", i, rnd.Intn(10000)),
			Function: gateway.ToolCallFunction{
				Name:      toolNames[rnd.Intn(len(toolNames))],
				Arguments: commands[rnd.Intn(len(commands))],
			},
		}
	}

	return ToolCallSequence{Calls: calls}
}

// extractCallID extracts the call_id from a tool event payload.
func extractCallID(payload string) string {
	var event struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return ""
	}
	return event.CallID
}

// runPropertyTest runs a property test with multiple random iterations.
// The property function returns true if the property holds, false otherwise.
// The property receives the harness RNG for reproducibility.
func runPropertyTest(t *testing.T, name string, iterations int, property func(ToolCallSequence, *rand.Rand) bool) {
	// Seed the random generator for reproducibility within this test
	seed := time.Now().UnixNano()
	rnd := rand.New(rand.NewSource(seed))

	t.Logf("Running property test %q with seed %d and %d iterations", name, seed, iterations)

	failures := 0
	var lastFailedInput ToolCallSequence

	for i := 0; i < iterations; i++ {
		// Generate random size between 1 and 15
		size := rnd.Intn(15) + 1
		seq := generateToolCallSequence(rnd, size)

		if !property(seq, rnd) {
			failures++
			lastFailedInput = seq
		}
	}

	if failures > 0 {
		t.Errorf("Property %q failed %d/%d times", name, failures, iterations)
		if len(lastFailedInput.Calls) > 0 {
			t.Logf("Example failed input: %d tool calls", len(lastFailedInput.Calls))
			for j, call := range lastFailedInput.Calls {
				t.Logf("  Call %d: id=%s, name=%s", j, call.ID, call.Function.Name)
			}
		}
	}
}

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

		// Process each tool call: emit TOOL_CALL then TOOL_RESULT (as in processAgentic)
		for _, call := range seq.Calls {
			w.emitToolCall(ctx, task, call)
			w.emitToolResult(ctx, task, call, `{"Success":true}`, 100)
		}

		// Verify: for each call_id, TOOL_CALL comes before TOOL_RESULT
		callPositions := make(map[string]int) // call_id -> TOOL_CALL position

		for i, ev := range sink.events {
			if ev.Type == models.EventTypeToolCall {
				callID := extractCallID(ev.Payload)
				callPositions[callID] = i
			} else if ev.Type == models.EventTypeToolResult {
				callID := extractCallID(ev.Payload)
				if pos, ok := callPositions[callID]; !ok {
					// No TOOL_CALL for this call_id - property violated
					return false
				} else if i <= pos {
					// TOOL_RESULT at index is not after TOOL_CALL - property violated
					return false
				}
			}
		}

		// Verify correct number of events (2 events per tool call)
		expectedEvents := len(seq.Calls) * 2
		if len(sink.events) != expectedEvents {
			return false
		}

		// Verify we have exactly len(seq.Calls) TOOL_CALL and len(seq.Calls) TOOL_RESULT
		toolCallCount := 0
		toolResultCount := 0
		for _, ev := range sink.events {
			if ev.Type == models.EventTypeToolCall {
				toolCallCount++
			} else if ev.Type == models.EventTypeToolResult {
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

		// Process each tool call
		for _, call := range seq.Calls {
			w.emitToolCall(ctx, task, call)
			w.emitToolResult(ctx, task, call, `{"Success":true}`, 100)
		}

		// Verify that call_id in TOOL_CALL matches call_id in TOOL_RESULT
		// Use pending map to ensure one-to-one pairing: each TOOL_CALL must have
		// exactly one TOOL_RESULT, no more, no less.
		pending := make(map[string]int) // call_id -> unmatched TOOL_CALL count

		for _, ev := range sink.events {
			callID := extractCallID(ev.Payload)
			if callID == "" {
				return false
			}

			if ev.Type == models.EventTypeToolCall {
				pending[callID]++
			} else if ev.Type == models.EventTypeToolResult {
				if pending[callID] == 0 {
					// No matching TOOL_CALL - property violated (result without call)
					return false
				}
				pending[callID]--
			}
		}

		// Ensure all TOOL_CALLs have exactly one TOOL_RESULT
		for _, unmatched := range pending {
			if unmatched != 0 {
				return false
			}
		}

		return true
	}

	runPropertyTest(t, "ToolCallIDMatching", iterations, property)
}

// TestEventOrderingWithVaryingCallCounts tests event ordering with different
// numbers of tool calls to ensure the property holds regardless of sequence length.
func TestEventOrderingWithVaryingCallCounts(t *testing.T) {
	// Test with specific counts to ensure edge cases are covered
	testCounts := []int{1, 2, 3, 5, 10, 15, 20}

	for _, count := range testCounts {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			seed := time.Now().UnixNano()
			rnd := rand.New(rand.NewSource(seed))

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

			// Process each tool call
			for _, call := range seq.Calls {
				w.emitToolCall(ctx, task, call)
				w.emitToolResult(ctx, task, call, `{"Success":true}`, 100)
			}

			// Verify ordering
			callPositions := make(map[string]int)
			for i, ev := range sink.events {
				if ev.Type == models.EventTypeToolCall {
					callID := extractCallID(ev.Payload)
					callPositions[callID] = i
				} else if ev.Type == models.EventTypeToolResult {
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

			// Verify event counts
			if len(sink.events) != count*2 {
				t.Errorf("Expected %d events, got %d", count*2, len(sink.events))
			}
		})
	}
}

// generateRandomArguments generates random argument strings of varying lengths.
func generateRandomArguments(rnd *rand.Rand) string {
	// Generate arguments of different lengths to test truncation
	lengthCategory := rnd.Intn(5)
	var args string

	switch lengthCategory {
	case 0:
		// Very short (0-50 chars)
		length := rnd.Intn(51)
		args = generateRandomJSON(rnd, length)
	case 1:
		// Short (50-150 chars)
		length := 50 + rnd.Intn(101)
		args = generateRandomJSON(rnd, length)
	case 2:
		// Medium (150-200 chars) - boundary case
		length := 150 + rnd.Intn(51)
		args = generateRandomJSON(rnd, length)
	case 3:
		// Long (200-500 chars) - will be truncated
		length := 200 + rnd.Intn(301)
		args = generateRandomJSON(rnd, length)
	case 4:
		// Very long (500-1000 chars) - will be truncated
		length := 500 + rnd.Intn(501)
		args = generateRandomJSON(rnd, length)
	}

	return args
}

// generateRandomJSON generates a random JSON-like string with approximately the target length.
func generateRandomJSON(rnd *rand.Rand, targetLength int) string {
	var b strings.Builder

	b.WriteString("{")
	// Add 2-5 key-value pairs
	numPairs := 2 + rnd.Intn(4)
	for i := 0; i < numPairs; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		// Generate a random key
		keyLength := 5 + rnd.Intn(10)
		key := randomString(rnd, keyLength)
		b.WriteString(`"` + key + `":"`)

		// Generate a value that helps reach target length
		remainingLength := targetLength - b.Len() - 2 // -2 for closing quotes and brace
		if remainingLength <= 0 {
			remainingLength = 10
		}
		valueLength := remainingLength / numPairs
		if valueLength < 1 {
			valueLength = 1
		}
		value := randomString(rnd, valueLength)
		b.WriteString(value + `"`)
	}
	b.WriteString("}")

	return b.String()
}

// randomString generates a random string of the given length (in runes).
// It uses a mix of ASCII and multibyte Unicode characters to test rune boundary handling.
func randomString(rnd *rand.Rand, length int) string {
	// Include ASCII and some Unicode characters (including multibyte)
	// Using runes ensures proper handling of multibyte characters
	runeSet := []rune{
		// ASCII letters (a-z, A-Z)
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
		'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
		'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
		// Digits (0-9)
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		// Some punctuation
		'_', '-',
		// Multibyte Unicode characters (e.g., accented letters, emoji)
		'é', 'ñ', 'ü', 'ö', 'ä', 'ß', 'ø', 'å', // Latin-1 Supplement
		'€', '£', '¥', '©', '®', '™',           // Symbols
		// Emoji (each is a multibyte rune - encode as string and convert to runes)
	}
	// Append emoji from string literals (can't be rune literals)
	emojiSet := []rune("😀🎉🚀💡⚡🔥")
	
	result := make([]rune, length)
	for i := 0; i < length; i++ {
		if rnd.Intn(10) < 7 { // 70% ASCII
			result[i] = runeSet[rnd.Intn(len(runeSet))]
		} else { // 30% emoji
			result[i] = emojiSet[rnd.Intn(len(emojiSet))]
		}
	}
	return string(result)
}

// extractResult wraps a value with an error to distinguish between
// missing fields and JSON decode failures.
type extractResult struct {
	value string
	err   error
}

// extractArgumentsSummary extracts the arguments_summary from a TOOL_CALL event payload.
// Returns an error if JSON decoding fails (not just if the field is missing).
func extractArgumentsSummary(payload string) extractResult {
	var event ToolCallEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return extractResult{value: "", err: err}
	}
	return extractResult{value: event.ArgumentsSummary, err: nil}
}

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

		// Override arguments with random ones
		for i := range seq.Calls {
			seq.Calls[i].Function.Arguments = generateRandomArguments(rnd)
		}

		// Emit TOOL_CALL for each tool call
		for _, call := range seq.Calls {
			w.emitToolCall(ctx, task, call)
		}

		// Verify: each TOOL_CALL event has arguments_summary <= 200 characters
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

		// Verify we emitted the correct number of TOOL_CALL events
		if len(sink.events) != len(seq.Calls) {
			return false
		}

		return true
	}

	runPropertyTest(t, "ArgumentsSummaryLengthBound", iterations, property)
}

// generateRandomOutput generates random tool output strings of varying lengths.
func generateRandomOutput(rnd *rand.Rand) string {
	// Generate outputs of different lengths to test truncation
	lengthCategory := rnd.Intn(6)
	var length int

	switch lengthCategory {
	case 0:
		// Very short (0-100 chars)
		length = rnd.Intn(101)
	case 1:
		// Short (100-500 chars)
		length = 100 + rnd.Intn(401)
	case 2:
		// Medium (500-900 chars)
		length = 500 + rnd.Intn(401)
	case 3:
		// Near boundary (900-1000 chars) - should not truncate
		length = 900 + rnd.Intn(101)
	case 4:
		// Just over boundary (1000-1500 chars) - will be truncated
		length = 1000 + rnd.Intn(501)
	case 5:
		// Very long (1500-3000 chars) - will be truncated
		length = 1500 + rnd.Intn(1501)
	}

	// Generate random output content
	content := randomString(rnd, length)
	return content
}

// extractOutputSummary extracts the output_summary from a TOOL_RESULT event payload.
// Returns an error if JSON decoding fails (not just if the field is missing).
func extractOutputSummary(payload string) extractResult {
	var event ToolResultEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return extractResult{value: "", err: err}
	}
	return extractResult{value: event.OutputSummary, err: nil}
}

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

		// Generate random outputs and emit TOOL_RESULT for each tool call
		for _, call := range seq.Calls {
			output := generateRandomOutput(rnd)
			w.emitToolResult(ctx, task, call, output, 100)
		}

		// Verify: each TOOL_RESULT event has output_summary <= 1000 characters
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
			// The max length includes the truncation suffix "...[truncated]" (14 chars)
			// So the actual max is maxOutputSummaryLength = 1000
			outputSummaryLength := utf8.RuneCountInString(outputSummaryResult.value)
			if outputSummaryLength > maxOutputSummaryLength {
				t.Logf("output_summary length: %d (max: %d)", outputSummaryLength, maxOutputSummaryLength)
				t.Logf("Payload: %s", ev.Payload)
				return false
			}
		}

		// Verify we emitted the correct number of TOOL_RESULT events
		if len(sink.events) != len(seq.Calls) {
			return false
		}

		return true
	}

	runPropertyTest(t, "OutputSummaryLengthBound", iterations, property)
}
