package truncation

import (
	"context"
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
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4", len(got))
	}
	if got[0].Role != "system" {
		t.Errorf("first message role = %q, want system", got[0].Role)
	}
	if got[1].Role != "user" {
		t.Errorf("second message role = %q, want user", got[1].Role)
	}
	if got[2].Role != "tool" {
		t.Errorf("third message role = %q, want tool (most recent)", got[2].Role)
	}
	if got[3].Role != "assistant" {
		t.Errorf("fourth message role = %q, want assistant (most recent)", got[3].Role)
	}
}

func TestAgenticTruncator_PreservesToolCallIDs(t *testing.T) {
	truncator := NewAgenticTruncator(4)
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
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4", len(got))
	}
	foundToolMsg := false
	for _, m := range got {
		if m.Role == "tool" && m.ToolCallID == "call_abc" {
			foundToolMsg = true
			break
		}
	}
	if !foundToolMsg {
		t.Error("expected tool message with tool_call_id to be preserved")
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
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			hasToolResponse := false
			for j := i + 1; j < len(got); j++ {
				if got[j].Role == "tool" && got[j].ToolCallID == m.ToolCalls[0].ID {
					hasToolResponse = true
					break
				}
			}
			if !hasToolResponse {
				t.Errorf("message %d has dangling ToolCall without tool response", i)
			}
		}
	}
}
