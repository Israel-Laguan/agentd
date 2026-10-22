package kanban

import (
	"context"
	"fmt"
	"time"
)

// PruneUsageBefore deletes ai_calls and tool_calls rows recorded before cutoff
// and reports how many rows went. Both tables are write-once, so there is
// nothing to keep but the time (T-056).
func (s *Store) PruneUsageBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	at := formatTime(cutoff)
	var removed int64
	for _, table := range []string{"ai_calls", "tool_calls"} {
		res, err := s.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE at < ?", at)
		if err != nil {
			return removed, fmt.Errorf("prune %s: %w", table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return removed, fmt.Errorf("prune %s rows: %w", table, err)
		}
		removed += n
	}
	return removed, nil
}
