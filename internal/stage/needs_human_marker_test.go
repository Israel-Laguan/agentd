package stage

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeTarget declares no needs_human field and fails validation without a
// name. A reply that only asks for a person must still reach the caller as
// OutcomeNeedsHuman: the marker survives the typed decode and skips validation.
func TestModelAgentKeepsNeedsHumanMarkerThroughTypedDecode(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"needs_human":true,"question":"Which repo?"}`}}

	res, err := NewModelAgent(gw).Call(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if res.Outcome != OutcomeNeedsHuman {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomeNeedsHuman)
	}
	if len(gw.seen) != 1 {
		t.Fatalf("gateway calls = %d, want 1: a marked reply is not a contract violation", len(gw.seen))
	}
	var body map[string]any
	if err := json.Unmarshal(res.Output, &body); err != nil || body["question"] != "Which repo?" {
		t.Fatalf("Output = %s, want the model's marked reply kept as sent", res.Output)
	}
}

func TestModelAgentFalseMarkerIsAnOrdinaryAnswer(t *testing.T) {
	t.Parallel()
	gw := &scriptedGateway{replies: []string{`{"needs_human":false,"name":"x"}`}}

	res, err := NewModelAgent(gw).Call(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if res.Outcome != OutcomeCompleted || string(res.Output) != `{"name":"x"}` {
		t.Fatalf("result = %q %s, want completed with the typed output", res.Outcome, res.Output)
	}
}
