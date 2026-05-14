package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"agentd/internal/config"
	"agentd/internal/gateway/spec"
	"agentd/internal/models"
)

type mockGateway struct{}

func (m *mockGateway) Generate(ctx context.Context, req spec.AIRequest) (spec.AIResponse, error) {
	if req.JSONMode {
		return spec.AIResponse{Content: `{"decisions_made": ["test decision"], "facts_established": ["test fact"]}`}, nil
	}
	return spec.AIResponse{Content: "normal response"}, nil
}
func (m *mockGateway) GeneratePlan(ctx context.Context, userIntent string) (*models.DraftPlan, error) {
	return nil, nil
}
func (m *mockGateway) AnalyzeScope(ctx context.Context, userIntent string) (*spec.ScopeAnalysis, error) {
	return nil, nil
}
func (m *mockGateway) ClassifyIntent(ctx context.Context, userIntent string) (*spec.IntentAnalysis, error) {
	return nil, nil
}

// newTestCM creates a ContextManager seeded with working zone messages
// for correction-related tests that don't need full config/gateway.
func newTestCM(seed []spec.PromptMessage) *ContextManager {
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "", "")
	cm.workingZone.Messages = append([]spec.PromptMessage(nil), seed...)
	return cm
}

// ---------------------------------------------------------------------------
// Structured Context Zone tests
// ---------------------------------------------------------------------------

func TestPartitionAnchor(t *testing.T) {
	cm := &ContextManager{}
	messages := []spec.PromptMessage{
		{Role: "system", Content: "sys1"},
		{Role: "system", Content: "sys2"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "ast1"},
		{Role: "tool", Content: "tool1"},
	}
	anchor, rest := cm.partitionAnchor(messages)
	if len(anchor) != 3 {
		t.Errorf("expected 3 anchor messages, got %d", len(anchor))
	}
	if anchor[0].Content != "sys1" || anchor[1].Content != "sys2" || anchor[2].Content != "user1" {
		t.Errorf("unexpected anchor content")
	}
	if len(rest) != 2 {
		t.Errorf("expected 2 remaining messages, got %d", len(rest))
	}
}

func TestGroupTurns(t *testing.T) {
	cm := &ContextManager{}
	messages := []spec.PromptMessage{
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "ast1", ToolCalls: []spec.ToolCall{{ID: "1"}}},
		{Role: "tool", ToolCallID: "1", Content: "res1"},
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "ast2"},
	}
	turns := cm.groupTurns(messages)
	if len(turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(turns))
	}
	if len(turns[0].Messages) != 3 {
		t.Errorf("expected 3 messages in turn 0, got %d", len(turns[0].Messages))
	}
	if len(turns[1].Messages) != 2 {
		t.Errorf("expected 2 messages in turn 1, got %d", len(turns[1].Messages))
	}
}

func TestRollingSummarizationTrigger(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 2,
		KeepRecentTurns:       1,
	}
	cm := NewContextManager(cfg, &mockGateway{}, "agent", "task")
	messages := []spec.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "ast1"},
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "ast2"},
		{Role: "user", Content: "user3"},
		{Role: "assistant", Content: "ast3"},
	}
	prepared, err := cm.PrepareContext(context.Background(), messages)
	if err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if len(prepared) != 5 {
		t.Errorf("expected 5 prepared messages, got %d", len(prepared))
	}
	foundSummary := false
	for _, m := range prepared {
		if m.Role == "system" && len(m.Content) > 0 && m.Content[0] == 'P' {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Errorf("summary message not found in prepared context")
	}
}

func TestSummarizationCaching(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 1,
		KeepRecentTurns:       1,
	}
	cm := NewContextManager(cfg, &mockGateway{}, "agent", "task")
	messages := []spec.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "ast1"},
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "ast2"},
	}
	if _, err := cm.PrepareContext(context.Background(), messages); err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if len(cm.summarizedTurns) != 1 {
		t.Errorf("expected 1 cached turn, got %d", len(cm.summarizedTurns))
	}
	if _, err := cm.PrepareContext(context.Background(), messages); err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if len(cm.summarizedTurns) != 1 {
		t.Errorf("expected still 1 cached turn, got %d", len(cm.summarizedTurns))
	}
}

func TestIncrementalCaching(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 1,
		KeepRecentTurns:       1,
	}
	cm := NewContextManager(cfg, &mockGateway{}, "agent", "task")
	messages := []spec.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: "ast1"},
		{Role: "user", Content: "user2"},
		{Role: "assistant", Content: "ast2"},
	}
	if _, err := cm.PrepareContext(context.Background(), messages); err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if len(cm.summarizedTurns) != 1 {
		t.Fatalf("expected 1 cached turn after first call, got %d", len(cm.summarizedTurns))
	}

	// Add a new turn — only the new turn should need summarization
	messages = append(messages,
		spec.PromptMessage{Role: "user", Content: "user3"},
		spec.PromptMessage{Role: "assistant", Content: "ast3"},
	)
	if _, err := cm.PrepareContext(context.Background(), messages); err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if len(cm.summarizedTurns) != 2 {
		t.Errorf("expected 2 cached turns after incremental call, got %d", len(cm.summarizedTurns))
	}
	if cm.runningSummary == nil {
		t.Error("expected running summary to be set")
	}
}

func TestBudgetEnforcement(t *testing.T) {
	cfg := config.AgenticContextConfig{
		AnchorBudget:          100,
		WorkingBudget:         100,
		CompressedBudget:      100,
		RollingThresholdTurns: 10,
	}
	cm := NewContextManager(cfg, &mockGateway{}, "agent", "task")
	longContent := strings.Repeat("A", 1000)
	messages := []spec.PromptMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "task"},
		{Role: "user", Content: "user1"},
		{Role: "assistant", Content: longContent},
	}
	prepared, err := cm.PrepareContext(context.Background(), messages)
	if err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	if totalChars(prepared) >= totalChars(messages) {
		t.Errorf("expected total characters to be reduced, but got %d >= %d", totalChars(prepared), totalChars(messages))
	}
	totalBudget := cfg.AnchorBudget + cfg.WorkingBudget + cfg.CompressedBudget
	if totalChars(prepared) > totalBudget {
		t.Errorf("prepared context exceeds budget: got %d > %d", totalChars(prepared), totalBudget)
	}
	if prepared[0].Content != "sys" || prepared[1].Content != "task" {
		t.Errorf("anchor messages were modified")
	}
}

// ---------------------------------------------------------------------------
// Correction injection tests (from main)
// ---------------------------------------------------------------------------

func TestCorrectionRecord_FormatMessage(t *testing.T) {
	rec := CorrectionRecord{
		Contradiction: "the server runs on port 3000",
		CorrectFact:   "the server runs on port 8080",
		Source:        CorrectionSourceTool,
	}
	msg := rec.FormatMessage()
	if !strings.HasPrefix(msg, "[CORRECTION]") {
		t.Fatalf("expected [CORRECTION] prefix, got %q", msg)
	}
	if !strings.Contains(msg, "the server runs on port 3000") {
		t.Fatal("expected contradiction text in message")
	}
	if !strings.Contains(msg, "the server runs on port 8080") {
		t.Fatal("expected correct fact text in message")
	}
}

func TestContextManager_InjectCorrection_PrependsToWorkingZone(t *testing.T) {
	cm := newTestCM([]spec.PromptMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "do the thing"},
	})
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "old fact",
		CorrectFact:   "new fact",
		Source:        CorrectionSourceHuman,
	})
	msgs := cm.WorkingMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if !IsCorrectionMessage(msgs[0].Content) {
		t.Fatalf("first message should be a correction, got %q", msgs[0].Content)
	}
	if msgs[0].Role != "system" {
		t.Fatalf("correction message should have role=system, got %q", msgs[0].Role)
	}
}

func TestContextManager_MultipleCorrections_NewestFirst(t *testing.T) {
	cm := newTestCM([]spec.PromptMessage{
		{Role: "user", Content: "hello"},
	})
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "first",
		CorrectFact:   "corrected-first",
		Source:        CorrectionSourceTool,
	})
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "second",
		CorrectFact:   "corrected-second",
		Source:        CorrectionSourceHuman,
	})
	msgs := cm.WorkingMessages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages (2 corrections + 1 seed), got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "second") {
		t.Fatalf("newest correction should be first, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "first") {
		t.Fatalf("older correction should be second, got %q", msgs[1].Content)
	}
	corrections := cm.Corrections()
	if len(corrections) != 2 {
		t.Fatalf("expected 2 correction records, got %d", len(corrections))
	}
	if corrections[0].Contradiction != "second" {
		t.Fatalf("corrections should be newest-first, got %q", corrections[0].Contradiction)
	}
}

func TestContextManager_InjectHumanCorrection(t *testing.T) {
	cm := newTestCM(nil)
	cm.InjectHumanCorrection("stale fact", "current fact")
	corrections := cm.Corrections()
	if len(corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(corrections))
	}
	if corrections[0].Source != CorrectionSourceHuman {
		t.Fatalf("expected human source, got %q", corrections[0].Source)
	}
}

func TestContextManager_InjectCorrection_DeduplicatesExactRecord(t *testing.T) {
	cm := newTestCM(nil)
	rec := CorrectionRecord{
		Contradiction: "old",
		CorrectFact:   "new",
		Source:        CorrectionSourceHuman,
	}
	cm.InjectCorrection(rec)
	rec.Timestamp = time.Now()
	cm.InjectCorrection(rec)

	corrections := cm.Corrections()
	if len(corrections) != 1 {
		t.Fatalf("expected 1 deduplicated correction, got %d", len(corrections))
	}
	msgs := cm.WorkingMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 correction message, got %d", len(msgs))
	}
}

func TestContextManager_InjectCorrection_DeduplicatesRenderedCorrection(t *testing.T) {
	cm := newTestCM(nil)
	if !cm.InjectCorrection(CorrectionRecord{
		Contradiction: "old",
		CorrectFact:   "new",
		Source:        CorrectionSourceHuman,
	}) {
		t.Fatal("expected first correction insert")
	}
	if cm.InjectCorrection(CorrectionRecord{
		Contradiction: "old",
		CorrectFact:   "new",
		Source:        CorrectionSourceTool,
	}) {
		t.Fatal("expected duplicate rendered correction to be skipped")
	}
}

func TestContextManager_Messages_CompressedThenWorking(t *testing.T) {
	cm := newTestCM([]spec.PromptMessage{
		{Role: "user", Content: "working message"},
	})
	cm.AddSummary(TurnSummary{
		Summary:          "summary of turns 1-5",
		FactsEstablished: []string{"port=3000"},
	})
	msgs := cm.Messages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Content != "summary of turns 1-5" {
		t.Fatalf("compressed zone should come first, got %q", msgs[0].Content)
	}
	if msgs[1].Content != "working message" {
		t.Fatalf("working zone should come second, got %q", msgs[1].Content)
	}
}

func TestContextManager_CheckToolResult_DetectsContradiction(t *testing.T) {
	cm := newTestCM([]spec.PromptMessage{
		{Role: "user", Content: "query"},
	})
	cm.AddSummary(TurnSummary{
		Summary:          "Server configured on port 3000",
		FactsEstablished: []string{"port=3000"},
	})
	detected := cm.CheckToolResult("port=8080")
	if len(detected) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(detected))
	}
	if detected[0].Source != CorrectionSourceTool {
		t.Fatalf("expected tool source, got %q", detected[0].Source)
	}
	msgs := cm.WorkingMessages()
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages after correction, got %d", len(msgs))
	}
	if !IsCorrectionMessage(msgs[0].Content) {
		t.Fatal("first working message should be a correction")
	}
}

func TestContextManager_CheckToolResult_NoContradiction(t *testing.T) {
	cm := newTestCM(nil)
	cm.AddSummary(TurnSummary{
		Summary:          "Server on port 3000",
		FactsEstablished: []string{"port=3000"},
	})
	detected := cm.CheckToolResult("port=3000")
	if len(detected) != 0 {
		t.Fatalf("expected no contradictions for matching value, got %d", len(detected))
	}
}

func TestIsCorrectionMessage(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "valid correction", content: "[CORRECTION] Earlier context may state: ...", want: true},
		{name: "leading whitespace", content: "  [CORRECTION] test", want: true},
		{name: "not a correction", content: "some regular message", want: false},
		{name: "empty string", content: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsCorrectionMessage(tt.content); got != tt.want {
				t.Fatalf("IsCorrectionMessage(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestContextManager_AppendWorking(t *testing.T) {
	cm := newTestCM([]spec.PromptMessage{
		{Role: "user", Content: "first"},
	})
	cm.AppendWorking(spec.PromptMessage{Role: "assistant", Content: "reply"})
	msgs := cm.WorkingMessages()
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[1].Content != "reply" {
		t.Fatalf("expected appended message at tail, got %q", msgs[1].Content)
	}
}

func TestInjectCorrection_AutoFillsTimestamp(t *testing.T) {
	cm := newTestCM(nil)
	before := time.Now()
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "a",
		CorrectFact:   "b",
		Source:        CorrectionSourceTool,
	})
	after := time.Now()
	rec := cm.Corrections()[0]
	if rec.Timestamp.Before(before) || rec.Timestamp.After(after) {
		t.Fatalf("timestamp %v should be between %v and %v", rec.Timestamp, before, after)
	}
}

func TestPrepareContext_InjectionPosition(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 10,
	}
	cm := NewContextManager(cfg, nil, "agent", "task")
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "stale fact",
		CorrectFact:   "new fact",
		Source:        CorrectionSourceHuman,
	})
	messages := []spec.PromptMessage{
		{Role: "system", Content: "primary prompt"},
		{Role: "user", Content: "first task"},
		{Role: "user", Content: "follow up"},
	}
	prepared, err := cm.PrepareContext(context.Background(), messages)
	if err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	// Expected: [system(primary), user(first task), system(correction), user(follow up)]
	// Wait, anchor is system + first user.
	if len(prepared) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(prepared))
	}
	if !IsCorrectionMessage(prepared[2].Content) {
		t.Fatalf("expected correction at index 2, got %q", prepared[2].Content)
	}
}

func TestPrepareContext_DoesNotAccumulateCorrectionMessages(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 10,
	}
	cm := NewContextManager(cfg, nil, "agent", "task")
	cm.InjectCorrection(CorrectionRecord{
		Contradiction: "stale fact",
		CorrectFact:   "new fact",
		Source:        CorrectionSourceHuman,
	})
	messages := []spec.PromptMessage{
		{Role: "system", Content: "primary prompt"},
		{Role: "user", Content: "first task"},
		{Role: "assistant", Content: "working"},
	}

	prepared, err := cm.PrepareContext(context.Background(), messages)
	if err != nil {
		t.Fatalf("PrepareContext failed: %v", err)
	}
	prepared, err = cm.PrepareContext(context.Background(), prepared)
	if err != nil {
		t.Fatalf("PrepareContext second call failed: %v", err)
	}

	var correctionCount int
	for _, msg := range prepared {
		if IsCorrectionMessage(msg.Content) {
			correctionCount++
		}
	}
	if correctionCount != 1 {
		t.Fatalf("expected exactly 1 correction message, got %d in %#v", correctionCount, prepared)
	}
}

func TestEnforceBudget_ProtectsCorrectionMessages(t *testing.T) {
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task")
	correction := CorrectionRecord{
		Contradiction: "stale",
		CorrectFact:   "current",
		Source:        CorrectionSourceHuman,
	}.FormatMessage()
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "system", Content: correction},
		{Role: "assistant", Content: strings.Repeat("x", 1000)},
	}

	prepared := cm.enforceBudget(messages, len("system")+len("task")+len(correction))
	if len(prepared) != 3 {
		t.Fatalf("expected only fixed messages, got %#v", prepared)
	}
	if !IsCorrectionMessage(prepared[2].Content) {
		t.Fatalf("expected protected correction, got %#v", prepared)
	}
}

func TestEnforceBudget_ProtectsCorrectionMessagesWithSummary(t *testing.T) {
	cm := NewContextManager(config.AgenticContextConfig{}, nil, "agent", "task")
	correction := CorrectionRecord{
		Contradiction: "stale",
		CorrectFact:   "current",
		Source:        CorrectionSourceHuman,
	}.FormatMessage()
	summary := "PREVIOUS CONTEXT SUMMARY\n- old work"
	messages := []spec.PromptMessage{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "task"},
		{Role: "system", Content: correction},
		{Role: "system", Content: summary},
		{Role: "assistant", Content: strings.Repeat("x", 1000)},
	}

	prepared := cm.enforceBudget(messages, len("system")+len("task")+len(correction)+len(summary))
	if len(prepared) != 4 {
		t.Fatalf("expected anchor, correction, and summary, got %#v", prepared)
	}
	if !IsCorrectionMessage(prepared[2].Content) {
		t.Fatalf("expected protected correction, got %#v", prepared)
	}
	if !strings.HasPrefix(prepared[3].Content, "PREVIOUS CONTEXT SUMMARY") {
		t.Fatalf("expected protected summary, got %#v", prepared)
	}
}

func TestParseCorrectionComment(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"valid", "[CORRECT] was: old; is: new", true},
		{"valid with leading whitespace", "  [CORRECT] was: old; is: new", true},
		{"valid with semicolon", "[CORRECT] was: old ; is: new ", true},
		{"valid values with semicolons", "[CORRECT] was: config=a;b; is: config=c;d", true},
		{"invalid format", "just a comment", false},
		{"missing is", "[CORRECT] was: old", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := ParseCorrectionComment(tt.body, CorrectionSourceHuman)
			if (rec != nil) != tt.want {
				t.Fatalf("ParseCorrectionComment() = %v, want %v", rec != nil, tt.want)
			}
			if tt.name == "valid values with semicolons" {
				if rec.Contradiction != "config=a;b" || rec.CorrectFact != "config=c;d" {
					t.Fatalf("unexpected semicolon record content: %+v", rec)
				}
				return
			}
			if tt.want && (rec.Contradiction != "old" || rec.CorrectFact != "new") {
				t.Fatalf("unexpected record content: %+v", rec)
			}
		})
	}
}

func TestContextManager_CheckToolResult_Deduplication(t *testing.T) {
	cm := newTestCM(nil)
	cm.AddSummary(TurnSummary{
		FactsEstablished: []string{"version=1.0.0"},
	})
	// First call detects and injects
	detected := cm.CheckToolResult("version=2.0.0")
	if len(detected) != 1 {
		t.Fatalf("expected 1 detection, got %d", len(detected))
	}
	// Second call should dedup
	detected2 := cm.CheckToolResult("version=2.0.0")
	if len(detected2) != 0 {
		t.Fatalf("expected 0 detections (dedup), got %d", len(detected2))
	}
}
