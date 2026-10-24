package worker

import (
	"context"
	"testing"

	"agentd/internal/gateway"
	"agentd/internal/models"
)

// truncatedBashCall is a bash call whose arguments were cut off mid-JSON, as a
// truncated provider response leaves them.
func truncatedBashCall(id string) gateway.AIResponse {
	return gateway.AIResponse{Content: "run", ToolCalls: []gateway.ToolCall{{
		ID: id, Type: "function",
		Function: gateway.ToolCallFunction{Name: "bash", Arguments: `{"command":"uname -`},
	}}}
}

// The agentic counterpart of the legacy invalid-JSON reply: a tool call whose
// arguments do not parse runs nothing, and the model sees the parse error and
// can send a valid call in the same loop.
func TestAgenticMalformedToolArgumentsReachTheModel(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		truncatedBashCall("c1"), bashCall("c2", "uname -a"), {Content: "Reported the kernel."},
	}}
	sb := &payloadCapturingSandbox{}
	w, _, task := runAgenticTask(t, gw, sb)

	if len(sb.payloads) != 1 || sb.payloads[0].Command != "uname -a" {
		t.Fatalf("sandbox payloads = %+v, want only the valid call to run", sb.payloads)
	}
	if gw.callCount != 3 || !requestContainsToolResult(gw.requests[1], "c1", "not valid JSON") {
		t.Fatalf("gateway calls = %d; the model must see the parse error for the malformed call", gw.callCount)
	}
	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateCompleted {
		t.Fatalf("state = %s, want COMPLETED after the valid call", got.State)
	}
}

// A model that keeps sending the same malformed call goes to a human instead of
// cycling through retries, as the legacy invalid-JSON handoff did (B-081).
func TestAgenticRepeatedMalformedToolArgumentsHandTaskToHuman(t *testing.T) {
	t.Parallel()
	gw := &sequenceGateway{responses: []gateway.AIResponse{
		truncatedBashCall("c1"), truncatedBashCall("c2"), truncatedBashCall("c3"), {Content: "done"},
	}}
	sb := &payloadCapturingSandbox{}
	w, _, task := runAgenticTask(t, gw, sb)

	got, err := w.store.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.State != models.TaskStateFailedRequiresHuman || got.RetryCount != 0 {
		t.Fatalf("state = %s retries = %d, want FAILED_REQUIRES_HUMAN with no retry", got.State, got.RetryCount)
	}
	if len(sb.payloads) != 0 {
		t.Fatalf("sandbox payloads = %+v, want no command to run", sb.payloads)
	}
}
