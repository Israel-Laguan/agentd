package context

import (
	"context"
	"testing"

	"agentd/internal/gateway/spec"
)

func TestInjectedCorrectionIsTaggedAsComments(t *testing.T) {
	cm := newTestCM([]spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "do the thing"},
	})
	cm.InjectCorrection(CorrectionRecord{Contradiction: "old", CorrectFact: "new", Source: CorrectionSourceHuman})
	prepared, err := cm.PrepareContext(context.Background(), cm.WorkingMessages())
	if err != nil {
		t.Fatal(err)
	}
	if got := prepared[2].Section; got != spec.SectionComments {
		t.Fatalf("correction section = %q, want %q", got, spec.SectionComments)
	}
}

func TestAddedSummaryIsTaggedAsHistory(t *testing.T) {
	cm := newTestCM(nil)
	cm.AddSummary(TurnSummary{Summary: "did things"})
	zone := cm.compressedZone.Messages
	if len(zone) != 1 || zone[0].Section != spec.SectionHistory {
		t.Fatalf("compressed zone = %+v, want one history message", zone)
	}
}
