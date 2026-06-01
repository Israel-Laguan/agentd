package agentic

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	agentcontext "agentd/internal/agent/context"
	"agentd/internal/gateway"
	"agentd/internal/models"
)

func testTruncationEngine(t *testing.T, truncatorMax, characterBudget int) *Engine {
	t.Helper()
	return &Engine{
		config: Config{
			TruncatorMax:    truncatorMax,
			CharacterBudget: characterBudget,
		},
	}
}

func syntheticLongHistory(n int) []gateway.PromptMessage {
	msgs := []gateway.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "task description"},
	}
	for i := 0; i < n; i++ {
		msgs = append(msgs, gateway.PromptMessage{Role: "user", Content: fmt.Sprintf("turn %d", i)})
	}
	return msgs
}

func syntheticToolExchangeHistory(exchanges int) []gateway.PromptMessage {
	msgs := []gateway.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
	}
	for i := 0; i < exchanges; i++ {
		id := fmt.Sprintf("call_%d", i)
		msgs = append(msgs,
			gateway.PromptMessage{
				Role:    "assistant",
				Content: fmt.Sprintf("calling %d", i),
				ToolCalls: []gateway.ToolCall{{
					ID:   id,
					Type: "function",
					Function: gateway.ToolCallFunction{
						Name:      "bash",
						Arguments: `{"command":"echo"}`,
					},
				}},
			},
			gateway.PromptMessage{Role: "tool", ToolCallID: id, Content: strings.Repeat("x", 200)},
			gateway.PromptMessage{Role: "assistant", Content: fmt.Sprintf("done %d", i)},
		)
	}
	return msgs
}

func TestApplyAgenticTruncation_UnderLimitNoOp(t *testing.T) {
	t.Parallel()

	e := testTruncationEngine(t, 30, 0)
	in := syntheticLongHistory(8)

	got, err := e.applyAgenticTruncation(context.Background(), in)
	if err != nil {
		t.Fatalf("applyAgenticTruncation() error = %v", err)
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatal("expected messages unchanged when under max and budget")
	}
}

func TestApplyAgenticTruncation_OverMaxMessages(t *testing.T) {
	t.Parallel()

	e := testTruncationEngine(t, 30, 0)
	in := syntheticLongHistory(60)

	got, err := e.applyAgenticTruncation(context.Background(), in)
	if err != nil {
		t.Fatalf("applyAgenticTruncation() error = %v", err)
	}
	if len(got) > 30 {
		t.Fatalf("len(got) = %d, want <= 30", len(got))
	}
	if got[0].Role != "system" || got[0].Content != "system prompt" {
		t.Fatalf("system anchor missing or changed: %#v", got[0])
	}
	if got[1].Role != "user" || got[1].Content != "task description" {
		t.Fatalf("first user anchor missing or changed: %#v", got[1])
	}
}

func TestApplyAgenticTruncation_InheritedGatewayCharacterBudget(t *testing.T) {
	t.Parallel()

	const gatewayDefault = 12000
	e := testTruncationEngine(t, 100, gatewayDefault)
	in := []gateway.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("z", 20000)},
	}

	got, err := e.applyAgenticTruncation(context.Background(), in)
	if err != nil {
		t.Fatalf("applyAgenticTruncation() error = %v", err)
	}
	if agentcontext.TotalChars(got) > gatewayDefault {
		t.Fatalf("totalChars(got) = %d, want <= %d (inherited gateway default)", agentcontext.TotalChars(got), gatewayDefault)
	}
}

func TestApplyAgenticTruncation_CharacterBudget(t *testing.T) {
	t.Parallel()

	e := testTruncationEngine(t, 100, 500)
	in := []gateway.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("z", 2000)},
	}

	got, err := e.applyAgenticTruncation(context.Background(), in)
	if err != nil {
		t.Fatalf("applyAgenticTruncation() error = %v", err)
	}
	if agentcontext.TotalChars(got) > 500 {
		t.Fatalf("totalChars(got) = %d, want <= 500", agentcontext.TotalChars(got))
	}
	if got[0].Role != "system" {
		t.Fatalf("system anchor missing: %#v", got[0])
	}
}

func TestApplyAgenticTruncation_ToolPairConsistency(t *testing.T) {
	t.Parallel()

	e := testTruncationEngine(t, 6, 0)
	in := syntheticToolExchangeHistory(8)

	got, err := e.applyAgenticTruncation(context.Background(), in)
	if err != nil {
		t.Fatalf("applyAgenticTruncation() error = %v", err)
	}
	verifyAgenticToolConsistency(t, got)
}

func TestBuildAgenticRequest_SkipTruncation(t *testing.T) {
	t.Parallel()

	e := &Engine{host: &noopHost{}}
	task := models.Task{BaseEntity: models.BaseEntity{ID: "t1"}, AgentID: "a1"}
	profile := models.AgentProfile{Provider: "openai", Model: "gpt-4"}

	req := e.buildAgenticRequest(task, profile, nil, nil, 0)
	if !req.SkipTruncation {
		t.Fatal("expected SkipTruncation=true on agentic AIRequest")
	}
}

func verifyAgenticToolConsistency(t *testing.T, messages []gateway.PromptMessage) {
	t.Helper()
	for i, m := range messages {
		if m.Role == "tool" {
			if !hasPrecedingAssistantCall(messages, i, m.ToolCallID) {
				t.Errorf("tool message %d has no preceding assistant tool call", i)
			}
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			for toolIdx, tc := range m.ToolCalls {
				if !hasSubsequentToolResponse(messages, i, tc.ID) {
					t.Errorf("assistant message %d tool call %d (ID %q) has no tool response", i, toolIdx, tc.ID)
				}
			}
		}
	}
}

func hasPrecedingAssistantCall(messages []gateway.PromptMessage, toolIndex int, toolCallID string) bool {
	for j := toolIndex - 1; j >= 0; j-- {
		if messages[j].Role != "assistant" {
			continue
		}
		for _, tc := range messages[j].ToolCalls {
			if tc.ID == toolCallID {
				return true
			}
		}
	}
	return false
}

func hasSubsequentToolResponse(messages []gateway.PromptMessage, assistantIndex int, toolCallID string) bool {
	for j := assistantIndex + 1; j < len(messages); j++ {
		if messages[j].Role == "tool" && messages[j].ToolCallID == toolCallID {
			return true
		}
	}
	return false
}
