package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// ============================================================================
// Tool Call Tests
// Tests tool call preservation and pairwise consistency
// ============================================================================

func TestAgenticTruncator_PreservesToolCallIDs(t *testing.T) {
	// Use budget 5 to keep all messages including the assistant+tool pair
	truncator := NewAgenticTruncator(5)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", ToolCalls: []spec.ToolCall{{ID: "call_abc", Type: "function", Function: spec.ToolCallFunction{Name: "test"}}}},
		{Role: "tool", ToolCallID: "call_abc", Content: "result"},
		{Role: "assistant", Content: "done"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("len(got) = %d, want 5", len(got))
	}

	// Verify system and user are retained
	if got[0].Role != "system" {
		t.Errorf("got[0].Role = %q, want system", got[0].Role)
	}
	if got[1].Role != "user" {
		t.Errorf("got[1].Role = %q, want user", got[1].Role)
	}

	// Verify assistant message with tool call is retained and has correct ID
	foundAssistantWithToolCall := false
	foundToolWithID := false
	for i, m := range got {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				if tc.ID == "call_abc" {
					foundAssistantWithToolCall = true
					// Verify corresponding tool message exists after this
					for j := i + 1; j < len(got); j++ {
						if got[j].Role == "tool" && got[j].ToolCallID == "call_abc" {
							foundToolWithID = true
							break
						}
					}
				}
			}
		}
	}
	if !foundAssistantWithToolCall {
		t.Error("expected assistant message with ToolCall ID call_abc to be preserved")
	}
	if !foundToolWithID {
		t.Error("expected tool message with ToolCallID call_abc to be preserved")
	}
}

func TestAgenticTruncator_PreventsDanglingToolCalls(t *testing.T) {
	truncator := NewAgenticTruncator(3)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "1", Content: "result 1"},
		{Role: "assistant", Content: "done"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	verifyToolConsistency(t, got)
}

func TestAgenticTruncator_PairwiseConsistency(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		{Role: "assistant", Content: "final"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify pairwise consistency: for every assistant with tool_calls, corresponding tool response exists
	for i, m := range got {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				hasResponse := false
				for j := i + 1; j < len(got); j++ {
					if got[j].Role == "tool" && got[j].ToolCallID == tc.ID {
						hasResponse = true
						break
					}
				}
				if !hasResponse {
					// If no response, tool_calls should be cleared and collapse marker added
					if len(m.ToolCalls) > 0 {
						t.Errorf("message %d has orphan tool_call %q without response", i, tc.ID)
					}
				}
			}
		}
	}
}

func TestAgenticTruncator_MultipleToolExchangesInSingleMessage(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "doing multiple things", ToolCalls: []spec.ToolCall{
			{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}},
			{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}},
			{ID: "call3", Type: "function", Function: spec.ToolCallFunction{Name: "baz"}},
		}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		{Role: "tool", ToolCallID: "call3", Content: "result 3"},
		{Role: "assistant", Content: "final response"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	verifyMultipleToolCallsConsistency(t, got)
}

func TestAgenticTruncator_OrphanedToolCallsRemoved(t *testing.T) {
	truncator := NewAgenticTruncator(3)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "done"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Should not have any assistant messages with orphaned tool_calls
	for _, m := range got {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Verify all tool_calls have corresponding tool responses
			for _, tc := range m.ToolCalls {
				hasResponse := false
				for _, m2 := range got {
					if m2.Role == "tool" && m2.ToolCallID == tc.ID {
						hasResponse = true
						break
					}
				}
				if !hasResponse {
					t.Errorf("assistant has orphan tool_call %q", tc.ID)
				}
			}
		}
	}
}

func TestAgenticTruncator_BudgetPreservesPairwiseConsistency(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "System"},
		{Role: "user", Content: "Task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: strings.Repeat("result ", 100)},
		{Role: "assistant", Content: "final"},
	}

	// Small budget that forces truncation
	budget := 50
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify pairwise consistency is maintained
	for i, m := range got {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for _, tc := range m.ToolCalls {
				hasResponse := false
				for j := i + 1; j < len(got); j++ {
					if got[j].Role == "tool" && got[j].ToolCallID == tc.ID {
						hasResponse = true
						break
					}
				}
				if !hasResponse && len(m.ToolCalls) > 0 {
					t.Errorf("message %d has orphan tool_call %q", i, tc.ID)
				}
			}
		}
	}
}

// Helper functions for tool call tests

// verifyToolConsistency checks that all tool messages have corresponding assistant calls
// and all assistant tool calls have corresponding tool responses
func verifyToolConsistency(t *testing.T, messages []spec.PromptMessage) {
	t.Helper()
	for i, m := range messages {
		if m.Role == "tool" {
			if !hasPrecedingAssistantCall(messages, i, m.ToolCallID) {
				t.Errorf("tool message %d has no preceding assistant tool call", i)
			}
		}

		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			verifyAssistantToolCallsHaveResponses(t, messages, i, m.ToolCalls)
		}
	}
}

// verifyAssistantToolCallsHaveResponses checks that all tool calls in an assistant message have responses
func verifyAssistantToolCallsHaveResponses(t *testing.T, messages []spec.PromptMessage, assistantIndex int, toolCalls []spec.ToolCall) {
	t.Helper()
	for toolIdx, tc := range toolCalls {
		if !hasSubsequentToolResponse(messages, assistantIndex, tc.ID) {
			t.Errorf("assistant message %d tool call index %d (ID: %q) has no subsequent tool response", assistantIndex, toolIdx, tc.ID)
		}
	}
}

// hasSubsequentToolResponse checks if there's a tool message after index i with the given tool call ID
func hasSubsequentToolResponse(messages []spec.PromptMessage, assistantIndex int, toolCallID string) bool {
	for j := assistantIndex + 1; j < len(messages); j++ {
		if messages[j].Role == "tool" && messages[j].ToolCallID == toolCallID {
			return true
		}
	}
	return false
}

// verifyMultipleToolCallsConsistency checks tool consistency for multiple tool calls in one message
func verifyMultipleToolCallsConsistency(t *testing.T, messages []spec.PromptMessage) {
	t.Helper()
	assistantMsg := findAssistantWithToolCalls(messages)
	if assistantMsg == nil {
		verifyCollapseMarkerWhenNoToolCalls(t, messages)
		return
	}

	verifyAllToolCallsHaveResponses(t, messages, assistantMsg.ToolCalls)
}

// findAssistantWithToolCalls finds the first assistant message that has tool calls
func findAssistantWithToolCalls(messages []spec.PromptMessage) *spec.PromptMessage {
	for i := range messages {
		if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
			return &messages[i]
		}
	}
	return nil
}

// verifyCollapseMarkerWhenNoToolCalls ensures collapse marker exists when tool calls are absent
func verifyCollapseMarkerWhenNoToolCalls(t *testing.T, messages []spec.PromptMessage) {
	t.Helper()
	found := false
	for _, m := range messages {
		if m.Role == "assistant" && len(m.ToolCalls) == 0 && containsCollapseMarker(m.Content) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected either assistant with tool_calls or collapse marker")
	}
}

// verifyAllToolCallsHaveResponses ensures every tool call has a corresponding response
func verifyAllToolCallsHaveResponses(t *testing.T, messages []spec.PromptMessage, toolCalls []spec.ToolCall) {
	t.Helper()
	for _, tc := range toolCalls {
		if !hasAnyToolResponse(messages, tc.ID) {
			t.Errorf("tool_call %q has no corresponding response", tc.ID)
		}
	}
}

// hasAnyToolResponse checks if any tool message in the slice has the given tool call ID
func hasAnyToolResponse(messages []spec.PromptMessage, toolCallID string) bool {
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID == toolCallID {
			return true
		}
	}
	return false
}
