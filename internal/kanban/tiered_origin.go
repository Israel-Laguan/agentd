package kanban

import (
	"context"
	"fmt"
	"time"

	"agentd/internal/models"
)

func (s *Store) CompleteTieredOrigin(
	ctx context.Context,
	id string,
	expectedUpdatedAt time.Time,
	result models.TaskResult,
) (*models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) (*models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin tiered origin completion: %w", err)
		}
		defer func() { _ = tx.Rollback() }()

		now := utcNow()
		// Guarantee the version moves forward even if the clock has not
		// ticked past the caller's read: without this, a claim that landed
		// in the same clock tick would leave updated_at unchanged and the
		// stale write below would still match.
		if !now.After(expectedUpdatedAt) {
			now = expectedUpdatedAt.Add(time.Nanosecond)
		}
		if err := completeTieredOriginState(ctx, tx, id, expectedUpdatedAt, result.Success, now); err != nil {
			return nil, err
		}
		if err := finishTaskResultSideEffects(ctx, tx, id, result, now); err != nil {
			return nil, err
		}
		task, err := selectTaskByID(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		return task, commitTx(tx, "tiered origin completion")
	})
}
