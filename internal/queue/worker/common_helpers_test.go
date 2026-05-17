package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

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
	seed := time.Now().UnixNano()
	rnd := rand.New(rand.NewSource(seed))

	t.Logf("Running property test %q with seed %d and %d iterations", name, seed, iterations)

	failures := 0
	var lastFailedInput ToolCallSequence

	for i := 0; i < iterations; i++ {
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

// generateRandomArguments generates random argument strings of varying lengths.
func generateRandomArguments(rnd *rand.Rand) string {
	lengthCategory := rnd.Intn(5)
	var args string

	switch lengthCategory {
	case 0:
		length := rnd.Intn(51)
		args = generateRandomJSON(rnd, length)
	case 1:
		length := 50 + rnd.Intn(101)
		args = generateRandomJSON(rnd, length)
	case 2:
		length := 150 + rnd.Intn(51)
		args = generateRandomJSON(rnd, length)
	case 3:
		length := 200 + rnd.Intn(301)
		args = generateRandomJSON(rnd, length)
	case 4:
		length := 500 + rnd.Intn(501)
		args = generateRandomJSON(rnd, length)
	}

	return args
}

// generateRandomJSON generates a random JSON-like string with approximately the target length.
func generateRandomJSON(rnd *rand.Rand, targetLength int) string {
	var b strings.Builder

	b.WriteString("{")
	numPairs := 2 + rnd.Intn(4)
	for i := 0; i < numPairs; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		keyLength := 5 + rnd.Intn(10)
		key := randomString(rnd, keyLength)
		b.WriteString(`"` + key + `":"`)

		remainingLength := targetLength - b.Len() - 2
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
func randomString(rnd *rand.Rand, length int) string {
	runeSet := []rune{
		'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm',
		'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z',
		'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M',
		'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z',
		'0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
		'_', '-',
		'é', 'ñ', 'ü', 'ö', 'ä', 'ß', 'ø', 'å',
		'€', '£', '¥', '©', '®', '™',
	}
	emojiSet := []rune("😀🎉🚀💡⚡🔥")

	result := make([]rune, length)
	for i := 0; i < length; i++ {
		if rnd.Intn(10) < 7 {
			result[i] = runeSet[rnd.Intn(len(runeSet))]
		} else {
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
func extractArgumentsSummary(payload string) extractResult {
	var event ToolCallEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return extractResult{value: "", err: err}
	}
	return extractResult{value: event.ArgumentsSummary, err: nil}
}

// generateRandomOutput generates random tool output strings of varying lengths.
func generateRandomOutput(rnd *rand.Rand) string {
	lengthCategory := rnd.Intn(6)
	var length int

	switch lengthCategory {
	case 0:
		length = rnd.Intn(101)
	case 1:
		length = 100 + rnd.Intn(401)
	case 2:
		length = 500 + rnd.Intn(401)
	case 3:
		length = 900 + rnd.Intn(101)
	case 4:
		length = 1000 + rnd.Intn(501)
	case 5:
		length = 1500 + rnd.Intn(1501)
	}

	return randomString(rnd, length)
}

// extractOutputSummary extracts the output_summary from a TOOL_RESULT event payload.
func extractOutputSummary(payload string) extractResult {
	var event ToolResultEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return extractResult{value: "", err: err}
	}
	return extractResult{value: event.OutputSummary, err: nil}
}
