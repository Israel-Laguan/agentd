package intake

import (
	"context"
	"strings"
	"testing"

	"agentd/internal/stage"
)

func TestClassifyIntent_PromptScopesStatusCheckToBoard(t *testing.T) {
	t.Parallel()
	gw := &fakeGateway{replies: []string{`{"intent":"plan_request","reason":"asks agent to find out"}`}}
	if _, err := ClassifyIntent(context.Background(), stage.NewModelAgent(gw), "what os and hardware have this machine"); err != nil {
		t.Fatalf("ClassifyIntent() error = %v", err)
	}
	system := gw.seen[0].Messages[0].Content
	for _, want := range []string{
		"board",
		"ONLY",
		"what os and hardware",
		"find out",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("intent prompt missing %q:\n%s", want, system)
		}
	}
	if !strings.Contains(strings.ToLower(system), "plan_request") {
		t.Fatalf("intent prompt must route find-out requests to plan_request:\n%s", system)
	}
}
