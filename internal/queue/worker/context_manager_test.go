package worker

import (
	"context"
	"strings"
	"testing"

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

func TestPrepareContextForceSummarize_BelowTurnThreshold(t *testing.T) {
	cfg := config.AgenticContextConfig{
		RollingThresholdTurns: 100,
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
	normal, err := cm.PrepareContext(context.Background(), messages)
	if err != nil {
		t.Fatalf("PrepareContext: %v", err)
	}
	if len(normal) != len(messages) {
		t.Fatalf("normal prepare flattened without summarize: got %d messages", len(normal))
	}
	forced, err := cm.PrepareContextForceSummarize(context.Background(), messages)
	if err != nil {
		t.Fatalf("PrepareContextForceSummarize: %v", err)
	}
	if len(forced) >= len(messages) {
		t.Fatalf("expected fewer messages after force summarize, got %d", len(forced))
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
