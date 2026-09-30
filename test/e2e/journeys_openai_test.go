//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestJ13_OpenAIIntake tests J13: the OpenAI-compatible intake at
// POST /v1/chat/completions.
//
// The journey spec only claimed "request parsed as OpenAI intake, response
// format matches OpenAI spec". The real surface is wider than that — the
// handler accepts the OpenAI `tools` / `tool_choice` opt-in and `stream`, and
// only emits `tool_calls` when the client declared the tool (see
// internal/api/controllers/chat_wire.go's buildToolCalls). So this journey
// pins the four things a client can actually depend on:
//
//  1. the non-streaming envelope (object, id, created, model, choices[0])
//  2. tool_calls, gated on a declared tool
//  3. tool_choice "none" suppressing those calls again
//  4. the stream:true framing, including the [DONE] sentinel
//
// plus the error intake, which is a 400 with a stable error code rather than
// a 200 with an error message — the one part of the spec where a client can
// branch reliably.
//
// Note `usage` is declared omitempty on the wire and never populated by the
// non-streaming path, so a client must treat it as optional. Asserting it
// would encode a gap as a contract; it is reported, not required.
func TestJ13_OpenAIIntake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	harness := NewHarness(baseURL, "default")
	if err := harness.WaitForHealthy(ctx, 10*time.Second); err != nil {
		t.Fatalf("J13 [boot] harness failed to become healthy: %v", err)
	}
	client := NewAPIClient(baseURL, harness.client)

	planMessage := j13PlanRequest()
	completion := j13AssertEnvelope(ctx, t, client, planMessage)
	j13AssertToolCalls(ctx, t, client, planMessage)
	j13AssertToolChoiceNoneSuppresses(ctx, t, client, planMessage)
	j13AssertStreaming(ctx, t, client, planMessage)
	j13AssertErrorIntake(ctx, t, client)

	if completion.Usage != nil {
		t.Logf("J13: usage block present (prompt=%d completion=%d total=%d)", completion.Usage.PromptTokens, completion.Usage.CompletionTokens, completion.Usage.TotalTokens)
	} else {
		t.Log("J13: no usage block — declared omitempty and never populated on the non-streaming path, so clients must treat it as optional")
	}
}

// j13PlanRequest returns a message the mock LLM answers with a DraftPlan
// (PLAN_KEYWORDS in devenv/mockllm/server.py), so the same message can drive
// both the plain-content and the tool_call assertions.
func j13PlanRequest() string {
	return "Create a project named " + UniqueProjectName("j13") + " that writes hello.txt to the workspace."
}

// j13AssertEnvelope is step 1: the response is a chat.completion, not an
// internal envelope. The daemon's own responses are wrapped in
// {"status":"success",...}; a leaked wrapper here would break every OpenAI
// client, so the absence of one is worth pinning.
func j13AssertEnvelope(ctx context.Context, t *testing.T, client *APIClient, message string) *ChatCompletion {
	t.Helper()

	resp, err := client.ChatCompletions(ctx, message, nil)
	if err != nil {
		t.Fatalf("J13 [chat] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J13 [chat] returned %d, want 200", resp.StatusCode)
	}
	completion, err := DecodeChatCompletion(resp)
	if err != nil {
		t.Fatalf("J13 [chat] decode failed: %v", err)
	}

	if completion.Object != "chat.completion" {
		t.Fatalf("J13 [chat] object = %q, want \"chat.completion\"", completion.Object)
	}
	if !strings.HasPrefix(completion.ID, "chatcmpl-") {
		t.Fatalf("J13 [chat] id = %q, want a \"chatcmpl-\" prefix", completion.ID)
	}
	if completion.Created <= 0 {
		t.Fatalf("J13 [chat] created = %d, want a positive unix timestamp", completion.Created)
	}
	if completion.Model == "" {
		t.Fatal("J13 [chat] model was empty, want the requested model echoed back")
	}
	if len(completion.Choices) != 1 {
		t.Fatalf("J13 [chat] choices = %d, want 1", len(completion.Choices))
	}
	choice := completion.Choices[0]
	if choice.Index != 0 {
		t.Fatalf("J13 [chat] choices[0].index = %d, want 0", choice.Index)
	}
	if choice.Message.Role != "assistant" {
		t.Fatalf("J13 [chat] choices[0].message.role = %q, want \"assistant\"", choice.Message.Role)
	}
	if choice.FinishReason != "stop" {
		t.Fatalf("J13 [chat] choices[0].finish_reason = %q, want \"stop\" (no tools declared)", choice.FinishReason)
	}
	if len(choice.Message.ToolCalls) != 0 {
		t.Fatalf("J13 [chat] got %d tool call(s) with no tools declared, want 0", len(choice.Message.ToolCalls))
	}

	// A plan-shaped intent answers with the DraftPlan as content; that is the
	// "chat completion" the feature file asserts, on the live stack.
	var plan DraftPlan
	if err := json.Unmarshal([]byte(choice.Message.Content), &plan); err != nil {
		t.Fatalf("J13 [chat] assistant content is not a plan: %v (content: %s)", err, choice.Message.Content)
	}
	if len(plan.Tasks) == 0 {
		t.Fatalf("J13 [chat] plan content had no tasks; content: %s", choice.Message.Content)
	}
	t.Logf("J13: chat.completion envelope valid (id=%s, model=%s, %d task(s) in content)", completion.ID, completion.Model, len(plan.Tasks))
	return completion
}

// j13AssertToolCalls is step 2: declaring a tool is what opts in to
// tool_calls, and the arguments carry the same plan JSON as the content.
func j13AssertToolCalls(ctx context.Context, t *testing.T, client *APIClient, message string) {
	t.Helper()

	resp, err := client.ChatCompletionsWithTools(ctx, message, []ToolDecl{j13CreatePlanTool()}, nil)
	if err != nil {
		t.Fatalf("J13 [chat tools] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J13 [chat tools] returned %d, want 200", resp.StatusCode)
	}
	completion, err := DecodeChatCompletion(resp)
	if err != nil {
		t.Fatalf("J13 [chat tools] decode failed: %v", err)
	}
	calls := completion.Choices[0].Message.ToolCalls
	if len(calls) != 1 {
		t.Fatalf("J13 [chat tools] got %d tool call(s), want 1 (arguments: %+v)", len(calls), completion.Choices[0].Message)
	}
	call := calls[0]
	if call.Type != "function" {
		t.Fatalf("J13 [chat tools] tool call type = %q, want \"function\"", call.Type)
	}
	if !strings.HasPrefix(call.ID, "call_") {
		t.Fatalf("J13 [chat tools] tool call id = %q, want a \"call_\" prefix", call.ID)
	}
	if call.Function.Name != "create_plan" {
		t.Fatalf("J13 [chat tools] tool call name = %q, want \"create_plan\"", call.Function.Name)
	}
	if completion.Choices[0].FinishReason != "tool_calls" {
		t.Fatalf("J13 [chat tools] finish_reason = %q, want \"tool_calls\"", completion.Choices[0].FinishReason)
	}
	// Arguments are a JSON *string*, per the OpenAI spec — a client has to
	// parse them rather than read them as an object.
	var plan DraftPlan
	if err := json.Unmarshal([]byte(call.Function.Arguments), &plan); err != nil {
		t.Fatalf("J13 [chat tools] tool call arguments are not a DraftPlan: %v (arguments: %s)", err, call.Function.Arguments)
	}
	if len(plan.Tasks) == 0 {
		t.Fatalf("J13 [chat tools] create_plan arguments carried no tasks; arguments: %s", call.Function.Arguments)
	}
	t.Logf("J13: tool_calls emitted for a declared tool (create_plan, %d task(s) in arguments)", len(plan.Tasks))
}

// j13AssertToolChoiceNoneSuppresses is step 3: a client that declares tools
// but asks for plain content gets no tool_calls. This is the flag's whole
// purpose, and the tool name is not the reason (it is declared either way) —
// tool_choice is.
func j13AssertToolChoiceNoneSuppresses(ctx context.Context, t *testing.T, client *APIClient, message string) {
	t.Helper()

	resp, err := client.ChatCompletionsWithTools(ctx, message, []ToolDecl{j13CreatePlanTool()}, "none")
	if err != nil {
		t.Fatalf("J13 [chat tool_choice=none] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J13 [chat tool_choice=none] returned %d, want 200", resp.StatusCode)
	}
	completion, err := DecodeChatCompletion(resp)
	if err != nil {
		t.Fatalf("J13 [chat tool_choice=none] decode failed: %v", err)
	}
	if len(completion.Choices[0].Message.ToolCalls) != 0 {
		t.Fatalf("J13 [chat tool_choice=none] got %d tool call(s), want 0", len(completion.Choices[0].Message.ToolCalls))
	}
	if completion.Choices[0].FinishReason != "stop" {
		t.Fatalf("J13 [chat tool_choice=none] finish_reason = %q, want \"stop\"", completion.Choices[0].FinishReason)
	}
	t.Log("J13: tool_choice=none suppressed tool_calls as specified")
}

// j13AssertStreaming is step 4: stream:true answers with
// chat.completion.chunk frames terminated by the [DONE] sentinel, with the
// role on the first frame and the finish reason on the last.
func j13AssertStreaming(ctx context.Context, t *testing.T, client *APIClient, message string) {
	t.Helper()

	resp, err := client.ChatCompletionsStream(ctx, message, nil)
	if err != nil {
		t.Fatalf("J13 [chat stream] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("J13 [chat stream] returned %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		_ = resp.Body.Close()
		t.Fatalf("J13 [chat stream] content-type = %q, want text/event-stream", ct)
	}

	frames, done, err := ReadChatStreamFrames(resp, 1<<20)
	if err != nil {
		t.Fatalf("J13 [chat stream] read failed: %v", err)
	}
	if !done {
		t.Fatal("J13 [chat stream] stream ended without the [DONE] sentinel")
	}
	if len(frames) < 2 {
		t.Fatalf("J13 [chat stream] got %d frame(s), want at least a role frame and a content frame", len(frames))
	}
	for i, frame := range frames {
		if frame.Object != "chat.completion.chunk" {
			t.Fatalf("J13 [chat stream] frame %d object = %q, want \"chat.completion.chunk\"", i, frame.Object)
		}
		if !strings.HasPrefix(frame.ID, "chatcmpl-") {
			t.Fatalf("J13 [chat stream] frame %d id = %q, want a \"chatcmpl-\" prefix", i, frame.ID)
		}
		if len(frame.Choices) == 0 {
			t.Fatalf("J13 [chat stream] frame %d had no choices", i)
		}
	}
	if frames[0].Choices[0].Delta.Role != "assistant" {
		t.Fatalf("J13 [chat stream] first frame role = %q, want \"assistant\"", frames[0].Choices[0].Delta.Role)
	}

	last := frames[len(frames)-1].Choices[0]
	if last.FinishReason == nil || *last.FinishReason != "stop" {
		t.Fatalf("J13 [chat stream] last frame finish_reason = %v, want \"stop\"", last.FinishReason)
	}
	var plan DraftPlan
	if err := json.Unmarshal([]byte(last.Delta.Content), &plan); err != nil || len(plan.Tasks) == 0 {
		t.Fatalf("J13 [chat stream] last frame content is not a plan: %v (content: %s)", err, last.Delta.Content)
	}
	t.Logf("J13: stream produced %d chat.completion.chunk frame(s) terminated by [DONE]", len(frames))
}

// j13AssertErrorIntake covers the rejection paths: each answers 400 with a
// stable error code, so a client can branch on them rather than parsing prose.
func j13AssertErrorIntake(ctx context.Context, t *testing.T, client *APIClient) {
	t.Helper()

	cases := []struct {
		name string
		body any
		want string
	}{
		{
			name: "no user message",
			body: map[string]any{"model": "agentd", "messages": []chatMessage{{Role: "system", Content: "you are a planning assistant"}}},
			want: "a user message is required",
		},
		{
			name: "multiple approved scopes",
			body: map[string]any{
				"model":           "agentd",
				"messages":        []chatMessage{{Role: "user", Content: "build both"}},
				"approved_scopes": []string{"backend-api", "frontend-ui"},
			},
			want: "",
		},
	}
	for _, tc := range cases {
		resp, err := client.PostJSON(ctx, "/v1/chat/completions", tc.body)
		if err != nil {
			t.Fatalf("J13 [chat %s] request failed: %v", tc.name, err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			_ = resp.Body.Close()
			t.Fatalf("J13 [chat %s] returned %d, want 400", tc.name, resp.StatusCode)
		}
		apiErr, err := DecodeError(resp)
		if err != nil {
			t.Fatalf("J13 [chat %s] decode failed: %v", tc.name, err)
		}
		if apiErr.Code != "BAD_REQUEST" {
			t.Fatalf("J13 [chat %s] error code = %q, want BAD_REQUEST (message: %s)", tc.name, apiErr.Code, apiErr.Message)
		}
		if tc.want != "" && !strings.Contains(apiErr.Message, tc.want) {
			t.Fatalf("J13 [chat %s] message = %q, want it to mention %q", tc.name, apiErr.Message, tc.want)
		}
		t.Logf("J13: %s correctly rejected: %s", tc.name, apiErr.Message)
	}

	// Malformed JSON never reaches the decoder that would produce an envelope.
	resp, err := client.Post(ctx, "/v1/chat/completions", map[string]string{"not": "a chat request"})
	if err != nil {
		t.Fatalf("J13 [chat malformed] request failed: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		_ = resp.Body.Close()
		t.Fatalf("J13 [chat malformed] returned %d, want 400", resp.StatusCode)
	}
	_ = resp.Body.Close()
}

func j13CreatePlanTool() ToolDecl {
	tool := ToolDecl{Type: "function"}
	tool.Function.Name = "create_plan"
	tool.Function.Description = "Propose a multi-task project plan for the user's request."
	return tool
}
