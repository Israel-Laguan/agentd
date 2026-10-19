package agentic

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	agentcontext "agentd/internal/agent/context"
	agentruntime "agentd/internal/agent/runtime"
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

type recordingGateway struct {
	sequenceGateway
	lastMessages []gateway.PromptMessage
}

func (g *recordingGateway) Generate(ctx context.Context, req gateway.AIRequest) (gateway.AIResponse, error) {
	g.lastMessages = req.Messages
	return g.sequenceGateway.Generate(ctx, req)
}

type noteTuningHost struct {
	*noopHost
	note string
}

func (h *noteTuningHost) ApplyTuning(req gateway.AIRequest, _ models.Task, _ models.AgentProfile, _ int) gateway.AIRequest {
	req.Messages = append(req.Messages, gateway.PromptMessage{Role: "user", Content: h.note})
	return req
}

// buildAgenticRequest applies tuning after the iteration pre-check, and a
// compressing retry can append a note. The request must be cut back to budget
// before generation, since SkipTruncation leaves the gateway's cut off. The cut
// comes out of working messages only: corrections and the summary are fixed
// context and must survive to the provider.
func TestGenerateAgenticTurn_TruncatesAfterTuning(t *testing.T) {
	const budget = 200
	const correction = "[CORRECTION] Earlier context may state: \"prior fact\". The correct information is: \"new fact\"."
	const summary = "PREVIOUS CONTEXT SUMMARY: earlier turns established the task context."
	gw := &recordingGateway{}
	e := &Engine{
		config: Config{Gateway: gw, TruncatorMax: 100, CharacterBudget: budget},
		host:   &noteTuningHost{noopHost: &noopHost{}, note: strings.Repeat("n", 500)},
	}
	messages := []gateway.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "system", Content: correction},
		{Role: "system", Content: summary},
		{Role: "tool", ToolCallID: "c1", Content: strings.Repeat("z", 500)},
	}

	_, stop, err := e.generateAgenticTurn(
		context.Background(), models.Task{BaseEntity: models.BaseEntity{ID: "t1"}},
		models.AgentProfile{}, &messages, nil,
		agentruntime.NewBudgetGuard(nil, ""), agentruntime.NewContextBudgetGuard(budget, 0),
		0, 0,
	)

	if err != nil || stop != nil {
		t.Fatalf("generateAgenticTurn: stop=%v err=%v", stop, err)
	}
	if agentcontext.TotalChars(gw.lastMessages) > budget {
		t.Fatalf("request has %d chars, over the %d budget after tuning", agentcontext.TotalChars(gw.lastMessages), budget)
	}
	if len(gw.lastMessages) == 0 || !strings.HasPrefix(gw.lastMessages[len(gw.lastMessages)-1].Content, "n") {
		t.Fatalf("tuning note missing from the generated request: %#v", gw.lastMessages)
	}
	if !containsMessage(gw.lastMessages, correction) {
		t.Fatalf("correction dropped from the generated request: %#v", gw.lastMessages)
	}
	if !containsMessage(gw.lastMessages, summary) {
		t.Fatalf("summary dropped from the generated request: %#v", gw.lastMessages)
	}
}

func containsMessage(messages []gateway.PromptMessage, content string) bool {
	for _, m := range messages {
		if m.Content == content {
			return true
		}
	}
	return false
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
