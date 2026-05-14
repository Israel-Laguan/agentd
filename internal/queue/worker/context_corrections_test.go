package worker

import (
	"context"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway/spec"
)

func TestInjectCorrection_ReturnsInsertStatus(t *testing.T) {
	cm := newTestCM(nil)
	rec := CorrectionRecord{
		Contradiction: "old",
		CorrectFact:   "new",
		Source:        CorrectionSourceHuman,
	}
	if !cm.InjectCorrection(rec) {
		t.Fatal("expected first correction insert to return true")
	}
	rec.Timestamp = time.Now()
	if cm.InjectCorrection(rec) {
		t.Fatal("expected duplicate correction insert to return false")
	}
}

func TestPrepareContext_StripsCorrectionsBeforeSummarization(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 1,
		KeepRecentTurns:       1,
	}
	cm := NewContextManager(cfg, &mockGateway{}, "agent", "task")
	rec := CorrectionRecord{
		Contradiction: "stale fact",
		CorrectFact:   "new fact",
		Source:        CorrectionSourceHuman,
	}
	if !cm.InjectCorrection(rec) {
		t.Fatal("expected correction insert to return true")
	}
	correctionMsg := spec.PromptMessage{
		Role:    "system",
		Content: rec.FormatMessage(),
	}
	cleanTurn := Turn{Messages: []spec.PromptMessage{
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "ast1"},
	}}
	messages := []spec.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		correctionMsg,
		cleanTurn.Messages[0],
		cleanTurn.Messages[1],
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "ast2"},
	}

	if _, err := cm.PrepareContext(context.Background(), messages); err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if len(cm.summarizedTurns) != 1 {
		t.Fatalf("expected only clean turn to be cached, got %d entries", len(cm.summarizedTurns))
	}
	if !cm.summarizedTurns[cm.hashTurn(cleanTurn)] {
		t.Fatal("expected correction-stripped turn hash to be cached")
	}
}

func TestContextManager_CheckToolResult_DeduplicatesWithinDetectionBatch(t *testing.T) {
	cm := newTestCM(nil)
	cm.AddSummary(TurnSummary{
		FactsEstablished: []string{"version=1.0.0", "version=1.0.0"},
	})

	detected := cm.CheckToolResult("version=2.0.0")
	if len(detected) != 1 {
		t.Fatalf("expected 1 inserted detection, got %d", len(detected))
	}
	if corrections := cm.Corrections(); len(corrections) != 1 {
		t.Fatalf("expected 1 stored correction, got %d", len(corrections))
	}
}
