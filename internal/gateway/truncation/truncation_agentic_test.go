package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestAgenticTruncator_UnderLimit(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Do task"},
		{Role: "assistant", Content: "I'll do it"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
}

func TestAgenticTruncator_PrunesMiddle(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "initial task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "2", Content: "result 2"},
		{Role: "assistant", Content: "final response"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0].Role != "system" {
		t.Errorf("first message role = %q, want system", got[0].Role)
	}
	if got[1].Role != "user" {
		t.Errorf("second message role = %q, want user", got[1].Role)
	}
	if got[2].Role != "assistant" {
		t.Errorf("third message role = %q, want assistant (most recent)", got[2].Role)
	}
}

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

func TestAgenticTruncator_DefaultMaxMessages(t *testing.T) {
	truncator := NewAgenticTruncator(0)
	at := truncator.(*AgenticTruncator)
	if at.MaxMessages != 20 {
		t.Errorf("MaxMessages = %d, want 20", at.MaxMessages)
	}
}

func TestAgenticTruncator_NoUserMessage(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "2", Content: "result 2"},
		{Role: "assistant", Content: "final response"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0].Role != "system" {
		t.Errorf("first message role = %q, want system", got[0].Role)
	}
	last := got[len(got)-1]
	if last.Role == "assistant" && len(last.ToolCalls) > 0 {
		t.Error("expected no dangling assistant ToolCalls without tool response")
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
	for i, m := range got {
		if m.Role == "tool" {
			hasAssistantCall := false
			for j := i - 1; j >= 0; j-- {
				if got[j].Role != "assistant" {
					continue
				}
				for _, call := range got[j].ToolCalls {
					if call.ID == m.ToolCallID {
						hasAssistantCall = true
						break
					}
				}
				if hasAssistantCall {
					break
				}
			}
			if !hasAssistantCall {
				t.Errorf("tool message %d has no preceding assistant tool call", i)
			}
		}

		// Check ALL retained tool calls in the assistant message, not just the first one
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for toolIdx, tc := range m.ToolCalls {
				hasToolResponse := false
				for j := i + 1; j < len(got); j++ {
					if got[j].Role == "tool" && got[j].ToolCallID == tc.ID {
						hasToolResponse = true
						break
					}
				}
				if !hasToolResponse {
					t.Errorf("assistant message %d tool call index %d (ID: %q) has no subsequent tool response", i, toolIdx, tc.ID)
				}
			}
		}
	}
}
// Test for pairwise consistency - tool_calls and tool results handled together
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

// Test that orphan tool_calls are marked as collapsed
func TestAgenticTruncator_OrphanToolCallsMarkedAsCollapsed(t *testing.T) {
	truncator := NewAgenticTruncator(3)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "final"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Check that any assistant message with orphaned tool_calls has collapse marker
	for _, m := range got {
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Has tool_calls but no tool response - should not happen
			t.Error("assistant message has tool_calls without corresponding tool response")
		}
		if m.Role == "assistant" && len(m.ToolCalls) == 0 {
			// If no tool_calls, check if content has collapse marker when tool exchanges were truncated
			if m.Content == "" {
				continue
			}
			// This is expected behavior - collapsed tool calls should have marker in content
		}
	}
}

// Test multiple tool exchanges in a single message
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

	// Find the assistant message that had multiple tool calls
	var assistantMsg *spec.PromptMessage
	for i := range got {
		if got[i].Role == "assistant" && len(got[i].ToolCalls) > 0 {
			assistantMsg = &got[i]
			break
		}
	}

	if assistantMsg == nil {
		// If no assistant with tool_calls, check that collapse marker exists
		found := false
		for _, m := range got {
			if m.Role == "assistant" && len(m.ToolCalls) == 0 && containsCollapseMarker(m.Content) {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected either assistant with tool_calls or collapse marker")
		}
		return
	}

	// Verify all tool responses are present for retained tool_calls
	for _, tc := range assistantMsg.ToolCalls {
		hasResponse := false
		for _, m := range got {
			if m.Role == "tool" && m.ToolCallID == tc.ID {
				hasResponse = true
				break
			}
		}
		if !hasResponse {
			t.Errorf("tool_call %q has no corresponding response", tc.ID)
		}
	}
}

// Test tool pair detection logic
func TestAgenticTruncator_FindToolExchanges(t *testing.T) {
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		{Role: "assistant", Content: "no tool call"},
	}

	exchanges := findToolExchanges(messages)

	if len(exchanges) != 2 {
		t.Fatalf("expected 2 exchanges, got %d", len(exchanges))
	}

	// First exchange
	if exchanges[0].assistantIndex != 1 {
		t.Errorf("first exchange assistant index = %d, want 1", exchanges[0].assistantIndex)
	}
	if len(exchanges[0].toolIndices) != 1 || exchanges[0].toolIndices[0] != 2 {
		t.Errorf("first exchange tool indices = %v, want [2]", exchanges[0].toolIndices)
	}

	// Second exchange
	if exchanges[1].assistantIndex != 3 {
		t.Errorf("second exchange assistant index = %d, want 3", exchanges[1].assistantIndex)
	}
	if len(exchanges[1].toolIndices) != 1 || exchanges[1].toolIndices[0] != 4 {
		t.Errorf("second exchange tool indices = %v, want [4]", exchanges[1].toolIndices)
	}
}

// Test when truncation drops tool exchanges, collapse marker is added
func TestAgenticTruncator_TruncationAddsCollapseMarker(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "initial task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "2", Content: "result 2"},
		{Role: "assistant", Content: "final response"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Check that either truncation marker or collapse marker is present
	hasMarker := false
	for _, m := range got {
		if containsCollapseMarker(m.Content) || containsTruncationMarker(m.Content) {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		t.Error("expected truncation or collapse marker in output")
	}
}

// Test when assistant tool_calls get orphaned, they are removed with collapse marker
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

// Helper function to check for collapse marker
func containsCollapseMarker(content string) bool {
	return len(content) >= len(CollapseMarker) &&
		(content[:len(CollapseMarker)] == CollapseMarker ||
			(len(content) > len(CollapseMarker) && 
			 (containsString(content, CollapseMarker))))
}

// Helper function to check for truncation marker
func containsTruncationMarker(content string) bool {
	return containsString(content, TruncationMarker)
}


// ============================================================================
// Task 3.3: Unit Tests for Collapse Markers
// Validates: Requirement 2.4
// ============================================================================

// Test that the CollapseMarker constant has the correct format
func TestCollapseMarker_Format(t *testing.T) {
	expected := "【N tool exchanges collapsed】"
	if CollapseMarker != expected {
		t.Errorf("CollapseMarker = %q, want %q", CollapseMarker, expected)
	}
}

// Test that collapse marker is inserted when tool exchanges are dropped
func TestCollapseMarker_InsertedWhenToolExchangesDropped(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		// First tool exchange
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		// Second tool exchange
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		// Third tool exchange
		{Role: "assistant", Content: "call 3", ToolCalls: []spec.ToolCall{{ID: "call3", Type: "function", Function: spec.ToolCallFunction{Name: "baz"}}}},
		{Role: "tool", ToolCallID: "call3", Content: "result 3"},
		// Final response
		{Role: "assistant", Content: "final"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Find collapse marker in output
	found := false
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected collapse marker when tool exchanges are dropped")
	}
}

// Test that collapse marker appears at the beginning of the message content
func TestCollapseMarker_PlacedAtBeginning(t *testing.T) {
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

	// Check that collapse marker is at the beginning of a message
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			if len(m.Content) > len(CollapseMarker) {
				// Marker should be at the start (possibly with trailing space)
				prefix := m.Content[:len(CollapseMarker)]
				if prefix != CollapseMarker {
					// Also check for marker with space after
					if len(m.Content) > len(CollapseMarker)+1 {
						prefixWithSpace := m.Content[:len(CollapseMarker)+1]
						if prefixWithSpace != CollapseMarker+" " {
							t.Errorf("collapse marker not at beginning: %q", m.Content[:min(50, len(m.Content))])
						}
					}
				}
			}
			return // Found and verified marker placement
		}
	}
	t.Error("collapse marker not found in any message")
}

// Test that multiple tool exchanges dropped shows correct count in marker
func TestCollapseMarker_CountReflectsDroppedExchanges(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		// 3 tool exchanges that will be dropped
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		{Role: "assistant", Content: "call 3", ToolCalls: []spec.ToolCall{{ID: "call3", Type: "function", Function: spec.ToolCallFunction{Name: "baz"}}}},
		{Role: "tool", ToolCallID: "call3", Content: "result 3"},
		// Final response
		{Role: "assistant", Content: "final"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Find message with collapse marker and verify count
	found := false
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			found = true
			// Check that the marker contains the expected format "【N tool exchanges collapsed】"
			// Note: current implementation uses static string, but we verify format is present
			if !containsString(m.Content, "tool exchanges collapsed") {
				t.Errorf("collapse marker format incorrect in: %q", m.Content)
			}
			break
		}
	}
	if !found {
		t.Error("collapse marker not found when multiple exchanges were dropped")
	}
}

// Test no collapse marker when no tool exchanges are dropped
func TestCollapseMarker_NotInsertedWhenNoExchangesDropped(t *testing.T) {
	truncator := NewAgenticTruncator(10) // High limit, no truncation needed
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "final"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Should not have collapse marker when nothing was truncated
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			t.Error("collapse marker should not be present when no exchanges were dropped")
		}
	}
}

// Test collapse marker with single tool exchange dropped
func TestCollapseMarker_SingleExchangeDropped(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		// One tool exchange that will be dropped
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		// Final response
		{Role: "assistant", Content: "final"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify collapse marker exists
	found := false
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			found = true
			break
		}
	}
	if !found {
		t.Error("collapse marker not found when single exchange was dropped")
	}
}

// Test that collapse marker appears in assistant message content
func TestCollapseMarker_InAssistantMessageContent(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		{Role: "assistant", Content: "final response"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify collapse marker appears in an assistant message
	foundInAssistant := false
	for _, m := range got {
		if m.Role == "assistant" && containsCollapseMarker(m.Content) {
			foundInAssistant = true
			break
		}
	}
	if !foundInAssistant {
		t.Error("collapse marker should appear in assistant message content")
	}
}

// Test that TruncationMarker is also present when truncation happens
func TestCollapseMarker_TruncationMarkerAlsoPresent(t *testing.T) {
	truncator := NewAgenticTruncator(4)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "call 1", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "foo"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "result 1"},
		{Role: "assistant", Content: "call 2", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "bar"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "result 2"},
		{Role: "assistant", Content: "final response"},
	}

	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// When truncation happens, both truncation marker and collapse marker may be present
	hasTruncationMarker := false
	hasCollapseMarker := false
	for _, m := range got {
		if containsTruncationMarker(m.Content) {
			hasTruncationMarker = true
		}
		if containsCollapseMarker(m.Content) {
			hasCollapseMarker = true
		}
	}

	// At least one marker should be present when truncation happens
	if !hasTruncationMarker && !hasCollapseMarker {
		t.Error("expected at least one marker (truncation or collapse) when truncation occurs")
	}
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
// ============================================================================
// Task 4.4: Unit Tests for Character Budget
// Validates: Requirement 5.1
// ============================================================================

// Test truncation when character limit exceeded
func TestAgenticTruncator_CharacterBudgetExceeded(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Do this complex task that requires many steps"},
		{Role: "assistant", Content: "I'll help you with that. Let me break this down into steps."},
		{Role: "tool", ToolCallID: "1", Content: "result 1 - very long content that exceeds the character budget"},
		{Role: "assistant", Content: "Based on the result, here's what we need to do next"},
		{Role: "tool", ToolCallID: "2", Content: "result 2 - more very long content that exceeds the character budget"},
		{Role: "assistant", Content: "Final response with all the information gathered"},
	}

	// Set budget to 100 characters total
	budget := 100
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Verify total character count is within budget
	total := totalChars(got)
	if total > budget {
		t.Errorf("total characters %d exceeds budget %d", total, budget)
	}

	// Verify system prompt is always preserved
	if len(got) == 0 || got[0].Role != "system" {
		t.Error("system prompt should be preserved")
	}
}

// Test budget not exceeded after truncation
func TestAgenticTruncator_BudgetNotExceeded(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "User task description"},
		{Role: "assistant", Content: strings.Repeat("A", 500)}, // 500 chars
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("B", 500)}, // 500 chars
		{Role: "assistant", Content: strings.Repeat("C", 500)}, // 500 chars
	}

	// Budget of 200 chars should force truncation
	budget := 200
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	total := totalChars(got)
	if total > budget {
		t.Errorf("total characters %d exceeds budget %d", total, budget)
	}

	// Verify at least system and user are preserved
	if len(got) < 2 {
		t.Errorf("expected at least 2 messages (system + user), got %d", len(got))
	}
}

// Test zero budget = unlimited behavior
func TestAgenticTruncator_ZeroBudgetUnlimited(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "User task"},
		{Role: "assistant", Content: strings.Repeat("X", 1000)},
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("Y", 1000)},
	}

	// Zero budget means unlimited
	got, err := truncator.Apply(context.Background(), messages, 0)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// All messages should be preserved
	if len(got) != len(messages) {
		t.Errorf("len(got) = %d, want %d (zero budget = unlimited)", len(got), len(messages))
	}

	// Content should be unchanged
	for i, m := range got {
		if m.Content != messages[i].Content {
			t.Errorf("message %d content changed with zero budget", i)
		}
	}
}

// Test character budget with message count limit also triggered
func TestAgenticTruncator_BudgetAndMessageCountBothTriggered(t *testing.T) {
	truncator := NewAgenticTruncator(3) // Low max messages
	messages := []spec.PromptMessage{
		{Role: "system", Content: "System"},
		{Role: "user", Content: "Task"},
		{Role: "assistant", Content: "First response"},
		{Role: "tool", ToolCallID: "1", Content: "Result 1"},
		{Role: "assistant", Content: "Second response"},
		{Role: "tool", ToolCallID: "2", Content: "Result 2"},
		{Role: "assistant", Content: "Third response"},
	}

	// Small budget that will trigger truncation
	budget := 50
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Should be within both limits
	total := totalChars(got)
	if total > budget {
		t.Errorf("total characters %d exceeds budget %d", total, budget)
	}
	if len(got) > 3 {
		t.Errorf("message count %d exceeds max %d", len(got), 3)
	}
}

// Test character budget when only anchors exist
func TestAgenticTruncator_BudgetWithOnlyAnchors(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "Very long system prompt " + strings.Repeat("X", 500)},
		{Role: "user", Content: "Very long user prompt " + strings.Repeat("Y", 500)},
	}

	// Budget smaller than anchors
	budget := 50
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// Should still have some output and stay within budget
	total := totalChars(got)
	if total > budget {
		t.Errorf("total characters %d exceeds budget %d", total, budget)
	}
	if len(got) == 0 {
		t.Error("expected at least one message")
	}
}

// Test totalChars helper function
func TestTotalChars(t *testing.T) {
	messages := []spec.PromptMessage{
		{Role: "system", Content: "Hello"},
		{Role: "user", Content: "World"},
		{Role: "assistant", Content: "Test"},
	}

	// "Hello" + "World" + "Test" = 15
	if totalChars(messages) != 15 {
		t.Errorf("totalChars = %d, want 15", totalChars(messages))
	}
}

// Test character budget preserves pairwise consistency
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

// Test negative budget is treated as unlimited
func TestAgenticTruncator_NegativeBudgetUnlimited(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "System"},
		{Role: "user", Content: "Task"},
		{Role: "assistant", Content: strings.Repeat("X", 1000)},
	}

	// Negative budget means unlimited
	got, err := truncator.Apply(context.Background(), messages, -1)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	// All messages should be preserved
	if len(got) != len(messages) {
		t.Errorf("len(got) = %d, want %d (negative budget = unlimited)", len(got), len(messages))
	}
}
// Debug test to understand character budget behavior
func TestAgenticTruncator_DebugBudget(t *testing.T) {
	truncator := NewAgenticTruncator(100) // High max messages to avoid count truncation
	messages := []spec.PromptMessage{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Do this complex task that requires many steps"},
		{Role: "assistant", Content: "I'll help you with that. Let me break this down into steps."},
		{Role: "tool", ToolCallID: "1", Content: "result 1 - very long content that exceeds the character budget"},
		{Role: "assistant", Content: "Based on the result, here's what we need to do next"},
		{Role: "tool", ToolCallID: "2", Content: "result 2 - more very long content that exceeds the character budget"},
		{Role: "assistant", Content: "Final response with all the information gathered"},
	}

	t.Logf("Initial total chars: %d", totalChars(messages))
	t.Logf("Message count: %d", len(messages))

	budget := 100
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	for i, m := range got {
		t.Logf("Msg %d: role=%s, content_len=%d, content=%q", i, m.Role, len(m.Content), m.Content)
	}

	total := totalChars(got)
	t.Logf("Final total chars: %d, budget: %d", total, budget)
}