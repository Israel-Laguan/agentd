package stage

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestModelAgentMakesOneGatewayCallForValidOutput(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"name":"agentd"}`}}
	res, err := NewModelAgent(gw).Call(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(gw.seen) != 1 {
		t.Fatalf("gateway calls = %d, want exactly 1", len(gw.seen))
	}
	if res.Outcome != OutcomeCompleted {
		t.Fatalf("outcome = %q, want %q", res.Outcome, OutcomeCompleted)
	}
	if string(res.Output) != `{"name":"agentd"}` {
		t.Fatalf("output = %s, want the validated JSON", res.Output)
	}
}

func TestModelAgentRetriesOnceOnContractViolation(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"name":""}`, `{"name":"agentd"}`}}
	if _, err := NewModelAgent(gw).Call(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}
	if len(gw.seen) != 2 {
		t.Fatalf("gateway calls = %d, want 2 (one corrective retry)", len(gw.seen))
	}
	if gw.seen[1].Attempt != spec.AttemptJSONRepair {
		t.Fatalf("retry attempt = %q, want %q", gw.seen[1].Attempt, spec.AttemptJSONRepair)
	}
}

func TestModelAgentForwardsRequestFields(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"name":"agentd"}`}}
	req := validRequest()
	req.Temperature = 0.2
	req.Context = []spec.PromptMessage{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}
	ctx := spec.WithTaskID(context.Background(), "T-057-task")
	if _, err := NewModelAgent(gw).Call(ctx, req); err != nil {
		t.Fatal(err)
	}
	got := gw.seen[0]
	if got.Purpose != req.Purpose {
		t.Errorf("purpose = %q, want %q", got.Purpose, req.Purpose)
	}
	if got.TaskID != "T-057-task" {
		t.Errorf("task id = %q, want the one carried by ctx", got.TaskID)
	}
	if got.Temperature != 0.2 {
		t.Errorf("temperature = %v, want 0.2", got.Temperature)
	}
	if got.Role != spec.RoleChat || !got.JSONMode {
		t.Errorf("role = %q json_mode = %v, want chat and JSON mode", got.Role, got.JSONMode)
	}
	if !reflect.DeepEqual(got.Messages, req.Context) {
		t.Errorf("messages = %+v, want %+v", got.Messages, req.Context)
	}
}

func TestModelAgentRejectsWorkspaceAccess(t *testing.T) {
	t.Parallel()
	for _, access := range []WorkspaceAccess{AccessReadOnly, AccessWrite} {
		gw := &scriptedGateway{replies: []string{`{"name":"agentd"}`}}
		req := validRequest()
		req.Access = access
		_, err := NewModelAgent(gw).Call(context.Background(), req)
		if !errors.Is(err, ErrModelAgentAccess) {
			t.Fatalf("access %v: error = %v, want ErrModelAgentAccess", access, err)
		}
		if len(gw.seen) != 0 {
			t.Fatalf("access %v: gateway was called %d times, want 0", access, len(gw.seen))
		}
	}
}

func TestModelAgentRejectsInvalidRequestWithoutCalling(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"name":"agentd"}`}}
	req := validRequest()
	req.Purpose = ""
	if _, err := NewModelAgent(gw).Call(context.Background(), req); err == nil {
		t.Fatal("want an error for a request without a purpose")
	}
	if len(gw.seen) != 0 {
		t.Fatalf("gateway calls = %d, want 0", len(gw.seen))
	}
}
