package kanban

import (
	"context"
	"testing"
	"time"

	"agentd/internal/models"
)

func aiCall(at time.Time, purpose, model string, tokens int) models.AICall {
	stage := purpose
	for i := range purpose {
		if purpose[i] == '.' {
			stage = purpose[:i]
			break
		}
	}
	return models.AICall{
		At: at, Kind: "generate", Purpose: purpose, Stage: stage, Provider: "p", Model: model,
		Attempt: "first", Outcome: "ok", Tokens: tokens, LatencyMS: 100,
	}
}

func TestInsertAICallsStoresEveryColumn(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	in := models.AICall{
		At: at, Kind: "generate", Purpose: "exec.turn", Stage: "exec", Role: "worker", TaskID: "t1",
		AgentID: "a1", CorrelationID: "req-1", Provider: "openai", Model: "m", Attempt: "json_repair",
		Cascade: true, Outcome: "invalid_json", Truncated: true, MessageCount: 3, ToolCount: 2,
		InputChars: 400, OutputChars: 50, Tokens: 120, CachedTokens: 30, CacheWriteTokens: 5, LatencyMS: 250,
	}
	if err := store.InsertAICalls(ctx, []models.AICall{in}); err != nil {
		t.Fatalf("InsertAICalls: %v", err)
	}
	var got models.AICall
	var atStr string
	var cascade, truncated int
	err := store.db.QueryRowContext(ctx, `SELECT at, kind, purpose, stage, role, task_id, agent_id, correlation_id,
		provider, model, attempt, cascade, outcome, truncated, message_count, tool_count, input_chars,
		output_chars, tokens, cached_tokens, cache_write_tokens, latency_ms FROM ai_calls`).Scan(
		&atStr, &got.Kind, &got.Purpose, &got.Stage, &got.Role, &got.TaskID, &got.AgentID, &got.CorrelationID,
		&got.Provider, &got.Model, &got.Attempt, &cascade, &got.Outcome, &truncated, &got.MessageCount,
		&got.ToolCount, &got.InputChars, &got.OutputChars, &got.Tokens, &got.CachedTokens, &got.CacheWriteTokens, &got.LatencyMS)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got.At, got.Cascade, got.Truncated = at, cascade == 1, truncated == 1
	if atStr != formatTime(at) {
		t.Fatalf("at = %q, want %q", atStr, formatTime(at))
	}
	if got != in {
		t.Fatalf("stored = %+v, want %+v", got, in)
	}
}

func TestInsertAICallsEmptyBatchIsANoop(t *testing.T) {
	t.Parallel()
	if err := newTestStore(t).InsertAICalls(context.Background(), nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
}
