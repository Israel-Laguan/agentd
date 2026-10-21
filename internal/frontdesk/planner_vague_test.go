package frontdesk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentd/internal/gateway/spec"
	"agentd/internal/testutil"
)

// B-085: "make my app better" was refused two ways ("I'm not sure what you
// need", "cannot be turned into a concrete software project") and neither
// asked anything specific. Both refusal paths must end with the same question.
func TestPlanner_VagueRequestRefusalsAskTheSameQuestion(t *testing.T) {
	messages := make([]string, 0, 2)
	for _, intent := range []string{"out_of_scope", "unclear"} {
		gw := &mockGateway{intent: &spec.IntentAnalysis{Intent: intent, Reason: "too vague"}}
		p := newTestPlanner(t, testutil.NewFakeStore(), gw)
		out, err := p.PlanContent(context.Background(), nil, "make my app better", nil)
		if err != nil {
			t.Fatalf("PlanContent(%s) error = %v", intent, err)
		}
		var reply struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(out, &reply); err != nil {
			t.Fatalf("unmarshal %s reply: %v", intent, err)
		}
		messages = append(messages, reply.Message)
	}
	question := commonSuffix(messages[0], messages[1])
	if !strings.Contains(question, "?") || len(question) < 40 {
		t.Fatalf("refusals share %q; want both to end with the same specific question:\n%s\n%s", question, messages[0], messages[1])
	}
}

func commonSuffix(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[len(a)-1-i] == b[len(b)-1-i] {
		i++
	}
	return a[len(a)-i:]
}
