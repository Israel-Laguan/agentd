package kanban

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

const insertAICallSQL = `INSERT INTO ai_calls (at, kind, purpose, stage, role, task_id, agent_id, correlation_id,
	provider, model, attempt, cascade, outcome, truncated, message_count, tool_count, input_chars,
	output_chars, tokens, cached_tokens, cache_write_tokens, latency_ms, context_mix, truncated_by)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// InsertAICalls writes a batch of call records in one transaction.
func (s *Store) InsertAICalls(ctx context.Context, calls []models.AICall) error {
	if len(calls) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin ai_calls insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, insertAICallSQL)
	if err != nil {
		return fmt.Errorf("prepare ai_calls insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, c := range calls {
		if _, err := stmt.ExecContext(ctx, aiCallArgs(c)...); err != nil {
			return fmt.Errorf("insert ai_call: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ai_calls insert: %w", err)
	}
	return nil
}

func aiCallArgs(c models.AICall) []any {
	return []any{
		formatTime(c.At), c.Kind, c.Purpose, c.Stage, c.RoutedAgent, c.TaskID, c.AgentID, c.CorrelationID,
		c.Provider, c.Model, c.Attempt, boolToInt(c.Cascade), c.Outcome, boolToInt(c.Truncated),
		c.MessageCount, c.ToolCount, c.InputChars, c.OutputChars, c.Tokens, c.CachedTokens,
		c.CacheWriteTokens, c.LatencyMS, c.ContextMix, c.TruncatedBy,
	}
}
