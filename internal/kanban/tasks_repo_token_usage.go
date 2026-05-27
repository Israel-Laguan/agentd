package kanban

import (
	"context"
	"fmt"
)

// AddTokenUsage atomically increments token_usage for the given task.
// It is a no-op when tokens <= 0 or the task does not exist.
func (s *Store) AddTokenUsage(ctx context.Context, taskID string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET token_usage = token_usage + ? WHERE id = ?`,
		tokens, taskID,
	)
	if err != nil {
		return fmt.Errorf("add token usage: %w", err)
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
