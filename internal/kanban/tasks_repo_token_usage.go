package kanban

import (
	"context"
	"fmt"

	"agentd/internal/gateway/spec"
)

// AddTokenUsage atomically increments token_usage for the given task.
// It is a no-op when tokens <= 0 and returns an error when taskID is unknown.
func (s *Store) AddTokenUsage(ctx context.Context, taskID string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET token_usage = token_usage + ? WHERE id = ?`,
		tokens, taskID,
	)
	if err != nil {
		return fmt.Errorf("add token usage: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("add token usage rows affected: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("add token usage: task %q not found", taskID)
	}
	return nil
}

// SumTokenUsage returns the sum of token_usage across all tasks.
func (s *Store) SumTokenUsage(ctx context.Context) (int, error) {
	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(token_usage), 0) FROM tasks`,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum token usage: %w", err)
	}
	return total, nil
}

// AddUsageDetails is an additive, forward-compatible seam on the token-usage
// store. M15 surfaces prompt-cache fields (cached_tokens / cache_write_tokens)
// via the TOKEN_USAGE event payload, which is the observability surface; it does
// not persist them to a dedicated DB column (deferred to a later milestone).
// This no-op keeps the TokenUsageStore interface additive without breaking
// existing callers of AddTokenUsage.
func (s *Store) AddUsageDetails(_ context.Context, _ string, _ spec.UsageDetails) error {
	return nil
}
