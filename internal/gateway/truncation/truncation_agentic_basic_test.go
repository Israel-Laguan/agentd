package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// ============================================================================
// Basic Functionality Tests
// Tests core truncation behavior without tool calls or budget constraints
// ============================================================================

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

	// Second exchange
	if exchanges[1].assistantIndex != 3 {
		t.Errorf("second exchange assistant index = %d, want 3", exchanges[1].assistantIndex)
	}
}

func TestTotalChars(t *testing.T) {
	messages := []spec.PromptMessage{
		{Role: "system", Content: "Hello"},
		{Role: "user", Content: "World"},
		{Role: "assistant", Content: "Test"},
	}

	// "Hello" + "World" + "Test" = 14 (5 + 5 + 4)
	if totalChars(messages) != 14 {
		t.Errorf("totalChars = %d, want 14", totalChars(messages))
	}
}

func TestAgenticTruncator_DebugBudget(t *testing.T) {
	truncator := NewAgenticTruncator(100) // High max messages to avoid count truncation
	messages := createDebugBudgetMessages()

	t.Logf("Initial total chars: %d", totalChars(messages))
	t.Logf("Message count: %d", len(messages))

	budget := 100
	got, err := truncator.Apply(context.Background(), messages, budget)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	verifyBudgetConstraints(t, got, budget)
	verifyAnchorMessagesPreserved(t, got)
	verifyNoOrphanedToolMessages(t, got)
}

// ============================================================================
// Helper Functions
// ============================================================================

func createDebugBudgetMessages() []spec.PromptMessage {
	return []spec.PromptMessage{
		{Role: "system", Content: "You are a helpful assistant"},
		{Role: "user", Content: "Do this complex task that requires many steps"},
		{Role: "assistant", Content: "I'll help you with that. Let me break this down into steps."},
		{Role: "tool", ToolCallID: "1", Content: "result 1 - very long content that exceeds the character budget"},
		{Role: "assistant", Content: "Based on the result, here's what we need to do next"},
		{Role: "tool", ToolCallID: "2", Content: "result 2 - more very long content that exceeds the character budget"},
		{Role: "assistant", Content: "Final response with all the information gathered"},
	}
}

func verifyBudgetConstraints(t *testing.T, messages []spec.PromptMessage, budget int) {
	t.Helper()
	resultTotal := totalChars(messages)
	if resultTotal > budget {
		t.Errorf("result total chars %d exceeds budget %d", resultTotal, budget)
	}
}

func verifyAnchorMessagesPreserved(t *testing.T, messages []spec.PromptMessage) {
	t.Helper()
	hasSystem := false
	hasUser := false
	for _, m := range messages {
		if m.Role == "system" {
			hasSystem = true
		}
		if m.Role == "user" {
			hasUser = true
		}
	}
	if !hasSystem {
		t.Error("system message should be preserved")
	}
	if !hasUser {
		t.Error("first user message should be preserved")
	}
}

func verifyNoOrphanedToolMessages(t *testing.T, messages []spec.PromptMessage) {
	t.Helper()
	for i, m := range messages {
		if m.Role == "tool" {
			if !hasPrecedingAssistantCall(messages, i, m.ToolCallID) {
				t.Error("orphaned tool message found")
			}
		}
	}
}

// hasPrecedingAssistantCall checks if there's an assistant message before index i with the given tool call ID
func hasPrecedingAssistantCall(messages []spec.PromptMessage, toolIndex int, toolCallID string) bool {
	for j := toolIndex - 1; j >= 0; j-- {
		if messages[j].Role == "assistant" {
			for _, call := range messages[j].ToolCalls {
				if call.ID == toolCallID {
					return true
				}
			}
		}
	}
	return false
}

// containsCollapseMarker checks if content contains a collapse marker
func containsCollapseMarker(content string) bool {
	return strings.Contains(content, "collapsed")
}

// containsTruncationMarker checks if content contains a truncation marker
func containsTruncationMarker(content string) bool {
	return strings.Contains(content, "【")
}
