package kanban

import (
	"context"
	"fmt"

	"agentd/internal/models"
)

// AddTokenUsage atomically increments token_usage for the given task.
// It is a no-op when tokens <= 0 and returns an error when taskID is unknown.
//
// The write is wrapped in retryOnBusy. Neither this statement nor AddUsageDetails
// is idempotent (both are read-modify-write accumulates, so a replay would
// double-count), but retrying SQLITE_BUSY is still correct: SQLite guarantees a
// write that returned BUSY did not commit, so there is no window where the row
// was written and the error reported anyway. Verified in
// TestSpikeBusyWriteUnderLongLock / TestSpikeBusyWriteWithRetryOnBusy.
func (s *Store) AddTokenUsage(ctx context.Context, taskID string, tokens int) error {
	if tokens <= 0 {
		return nil
	}
	var affected int64
	err := retryOnBusyNoResult(ctx, func(ctx context.Context) error {
		res, err := s.db.ExecContext(ctx,
			`UPDATE tasks SET token_usage = token_usage + ? WHERE id = ?`,
			tokens, taskID,
		)
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("add token usage: %w", err)
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

// AddUsageDetails atomically increments the cached token columns for the given task.
// It is a no-op when both values are <= 0 and returns an error when taskID is unknown.
// Retried on BUSY for the same reason as AddTokenUsage.
func (s *Store) AddUsageDetails(ctx context.Context, taskID string, details models.UsageDetails) error {
	cached := details.CachedTokens
	write := details.CacheWriteTokens
	if cached < 0 {
		cached = 0
	}
	if write < 0 {
		write = 0
	}
	if cached <= 0 && write <= 0 {
		return nil
	}
	var affected int64
	err := retryOnBusyNoResult(ctx, func(ctx context.Context) error {
		res, err := s.db.ExecContext(ctx,
			`UPDATE tasks SET cached_token_usage = cached_token_usage + ?, cache_write_token_usage = cache_write_token_usage + ? WHERE id = ?`,
			cached, write, taskID,
		)
		if err != nil {
			return err
		}
		affected, err = res.RowsAffected()
		return err
	})
	if err != nil {
		return fmt.Errorf("add usage details: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("add usage details: task %q not found", taskID)
	}
	return nil
}
