package truncation

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
)

// ============================================================================
// Budget Tests
// Tests character budget enforcement and truncation behavior
// ============================================================================

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

func TestAgenticTruncator_BudgetNotExceeded(t *testing.T) {
	truncator := NewAgenticTruncator(20)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "User task description"},
		{Role: "assistant", Content: strings.Repeat("A", 50)},             // 50 chars
		{Role: "tool", ToolCallID: "1", Content: strings.Repeat("B", 50)}, // 50 chars
		{Role: "assistant", Content: strings.Repeat("C", 50)},             // 50 chars
	}

	// Budget of 300 chars exceeds total ~200 chars, so no truncation should occur
	budget := 300
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
