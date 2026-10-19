package context

import (
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/gateway/truncation"
)

// The newest working messages, usually the latest tool result, are what the
// agent needs next, so the cut keeps them and drops or cuts older ones.
func TestTruncateWorkingMessages_KeepsNewestFirst(t *testing.T) {
	working := []spec.PromptMessage{
		{Role: "assistant", Content: strings.Repeat("o", 300)},
		{Role: "assistant", Content: strings.Repeat("n", 300)},
	}

	got := TruncateWorkingMessages(working, 300, truncation.MiddleOutStrategy{})

	if len(got) != 1 || got[0].Content != working[1].Content {
		t.Fatalf("kept %d messages starting %q, want only the newest, whole", len(got), firstChars(got))
	}
}

// An assistant tool call and its tool results are cut together: no tool
// result without its call, no call without its result.
func TestTruncateWorkingMessages_KeepsToolCallPairs(t *testing.T) {
	working := []spec.PromptMessage{
		toolCallMsg("a"), {Role: "tool", ToolCallID: "a", Content: strings.Repeat("A", 500)},
		toolCallMsg("b"), {Role: "tool", ToolCallID: "b", Content: strings.Repeat("B", 500)},
	}

	got := TruncateWorkingMessages(working, 600, truncation.MiddleOutStrategy{})

	assertToolPairs(t, got)
	if last := got[len(got)-1]; last.ToolCallID != "b" || last.Content != working[3].Content {
		t.Fatalf("newest tool result not kept whole: %+v", last)
	}
	if TotalChars(got) > 600 {
		t.Fatalf("cut context is %d chars, over the 600 budget", TotalChars(got))
	}
}

func toolCallMsg(id string) spec.PromptMessage {
	call := spec.ToolCall{ID: id}
	call.Function.Name = "bash"
	call.Function.Arguments = `{"command":"ls"}`
	return spec.PromptMessage{Role: "assistant", ToolCalls: []spec.ToolCall{call}}
}

func assertToolPairs(t *testing.T, msgs []spec.PromptMessage) {
	t.Helper()
	calls, results := map[string]bool{}, map[string]bool{}
	for _, m := range msgs {
		for _, c := range m.ToolCalls {
			calls[c.ID] = true
		}
		if m.Role == "tool" {
			if !calls[m.ToolCallID] {
				t.Fatalf("tool result %s kept without its call", m.ToolCallID)
			}
			results[m.ToolCallID] = true
		}
	}
	for id := range calls {
		if !results[id] {
			t.Fatalf("tool call %s kept without its result", id)
		}
	}
}

func firstChars(msgs []spec.PromptMessage) string {
	if len(msgs) == 0 || len(msgs[0].Content) < 3 {
		return ""
	}
	return msgs[0].Content[:3]
}
