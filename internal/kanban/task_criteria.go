package kanban

import (
	"context"
	"encoding/json"
	"fmt"
)

// UpdateCriteriaMet persists the met success criteria for a running task.
// It is a best-effort write: if the task is no longer in the RUNNING state
// (e.g. it was cancelled between turns) the update is silently skipped.
func (s *Store) UpdateCriteriaMet(ctx context.Context, id string, met []string) error {
	if met == nil {
		met = []string{}
	}
	encoded, err := json.Marshal(met)
	if err != nil {
		return fmt.Errorf("encode criteria_met: %w", err)
	}
	return retryOnBusyNoResult(ctx, func(ctx context.Context) error {
		_, err := s.db.ExecContext(ctx,
			`UPDATE tasks SET criteria_met = ?, updated_at = ? WHERE id = ? AND state = 'RUNNING'`,
			string(encoded), formatTime(utcNow()), id)
		if err != nil {
			return fmt.Errorf("update criteria_met for task %q: %w", id, err)
		}
		return nil
	})
}
