package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// B-079 review: with two results for one assistant call, the oversized newest
// result is cut first. The room reserved for the call must survive the older
// result, or the call is dropped and the cut result goes with it.
func TestAgenticTruncatorKeepsReservationAcrossSiblingResults(t *testing.T) {
	t.Parallel()
	msgs := []spec.PromptMessage{
		{Role: "system", Content: strings.Repeat("s", 975)},
		{Role: "user", Content: strings.Repeat("u", 238)},
		{Role: "assistant", ToolCalls: []spec.ToolCall{
			{ID: "c0", Type: "function", Function: spec.ToolCallFunction{Name: "bash", Arguments: `{"command":"a"}`}},
			{ID: "c1", Type: "function", Function: spec.ToolCallFunction{Name: "bash", Arguments: `{"command":"b"}`}},
		}},
		{Role: "tool", ToolCallID: "c0", Content: strings.Repeat("o", 20)},
		{Role: "tool", ToolCallID: "c1", Content: "HEAD-MARK\n" + strings.Repeat("filler\n", 8000)},
	}
	out, err := NewAgenticTruncator(0).Apply(context.Background(), msgs, 12000)
	if err != nil {
		t.Fatal(err)
	}
	var call *spec.PromptMessage
	var cut *spec.PromptMessage
	for i := range out {
		switch {
		case out[i].Role == "assistant" && len(out[i].ToolCalls) > 0:
			call = &out[i]
		case out[i].Role == "tool" && out[i].ToolCallID == "c1":
			cut = &out[i]
		}
	}
	if call == nil || cut == nil {
		t.Fatalf("lost the call or its cut result: %d messages", len(out))
	}
	found := false
	for _, tc := range call.ToolCalls {
		found = found || tc.ID == "c1"
	}
	if !found || !strings.HasPrefix(cut.Content, "HEAD-MARK") {
		t.Fatalf("cut result not paired with its call: calls=%+v", call.ToolCalls)
	}
	if got := totalChars(out); got > 12000 {
		t.Fatalf("total %d over budget", got)
	}
}
