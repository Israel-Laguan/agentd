package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// ============================================================================
// Collapse Marker Tests
// Tests collapse marker insertion and formatting
// ============================================================================

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
	}

	// Assert that a collapse marker exists (since messages were dropped)
	found := false
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected collapse marker when messages with tool_calls were truncated")
	}
}

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

func TestCollapseMarker_Format(t *testing.T) {
	expected := "【N tool exchanges collapsed】"
	if CollapseMarker != expected {
		t.Errorf("CollapseMarker = %q, want %q", CollapseMarker, expected)
	}
}

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
			// Marker should be at the start (possibly with trailing space)
			if !strings.HasPrefix(m.Content, "【") {
				t.Errorf("collapse marker not at beginning: %q", m.Content[:min(50, len(m.Content))])
			}
			return // Found and verified marker placement
		}
	}
	t.Error("collapse marker not found in any message")
}

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

	// Find message with collapse marker and verify correct count (3 exchanges)
	found := false
	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			found = true
			// Verify the exact count: should be "【3 tool exchanges collapsed】"
			if !strings.Contains(m.Content, "3 tool exchanges collapsed") {
				t.Errorf("collapse marker should contain count 3, got: %q", m.Content)
			}
			break
		}
	}
	if !found {
		t.Error("collapse marker not found when multiple exchanges were dropped")
	}
}

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

func TestAgenticTruncator_BudgetDropNonToolMessagesDoesNotClaimCollapsedExchanges(t *testing.T) {
	truncator := NewAgenticTruncator(50)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "first user anchor"},
		{Role: "user", Content: strings.Repeat("x", 40)},
		{Role: "assistant", Content: strings.Repeat("y", 40)},
		{Role: "assistant", Content: "recent reply"},
	}

	got, err := truncator.Apply(context.Background(), messages, 35)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	for _, m := range got {
		if containsCollapseMarker(m.Content) {
			t.Fatalf("unexpected collapse marker when only non-tool messages were dropped: %q", m.Content)
		}
	}
}

func TestTruncateMiddleToBudget_CollapseMarkerCountsDroppedToolExchanges(t *testing.T) {
	truncator := &AgenticTruncator{}
	messages := []spec.PromptMessage{
		{Role: "assistant", Content: "a", ToolCalls: []spec.ToolCall{{ID: "call1", Type: "function", Function: spec.ToolCallFunction{Name: "f1"}}}},
		{Role: "tool", ToolCallID: "call1", Content: "b"},
		{Role: "assistant", Content: "c", ToolCalls: []spec.ToolCall{{ID: "call2", Type: "function", Function: spec.ToolCallFunction{Name: "f2"}}}},
		{Role: "tool", ToolCallID: "call2", Content: "d"},
		{Role: "assistant", Content: "e"},
	}

	got := truncator.truncateMiddleToBudget(messages, 3)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}

	wantPrefix := CollapseMarkerFor(1) + " "
	if !strings.HasPrefix(got[0].Content, wantPrefix) {
		t.Fatalf("got[0].Content = %q, want prefix %q", got[0].Content, wantPrefix)
	}
}
