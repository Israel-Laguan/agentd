package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// B-079: a tool result larger than the whole budget is cut, not dropped with
// its call. Before the fix the result took all the budget, the assistant call
// was dropped, and the dangling-result cleanup then removed the result too, so
// the agent never saw it and called the tool again until the iteration limit.
func TestAgenticTruncatorCutsOversizedToolResultKeepingItsCall(t *testing.T) {
	t.Parallel()
	result := "HEAD-MARK\n" + strings.Repeat("filler\n", 8000) + "TAIL-MARK\n"
	msgs := []spec.PromptMessage{
		{Role: "system", Content: strings.Repeat("s", 975)},
		{Role: "user", Content: strings.Repeat("u", 238)},
		{Role: "assistant", ToolCalls: []spec.ToolCall{{ID: "c0", Type: "function", Function: spec.ToolCallFunction{Name: "bash", Arguments: `{"command":"x"}`}}}},
		{Role: "tool", ToolCallID: "c0", Content: result},
	}
	const budget = 12000
	out, err := NewAgenticTruncator(0).Apply(context.Background(), msgs, budget)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 {
		t.Fatalf("got %d messages, want system, user, assistant call and cut result", len(out))
	}
	call, res := out[2], out[3]
	if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].ID != "c0" {
		t.Fatalf("assistant call lost: %+v", call)
	}
	if res.Role != "tool" || res.ToolCallID != "c0" || !strings.HasPrefix(res.Content, "HEAD-MARK") || !strings.Contains(res.Content, TruncationMarker) {
		t.Fatalf("tool result not cut in place: role=%s id=%s len=%d", res.Role, res.ToolCallID, len(res.Content))
	}
	if got := totalChars(out); got > budget {
		t.Fatalf("total %d chars over budget %d", got, budget)
	}
}
