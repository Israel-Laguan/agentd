package kanban

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"agentd/internal/models"
)

const (
	hitlExpiresAtPrefix     = "agentd:hitl:expires-at:"
	hitlTimeoutEventPayload = "human-in-the-loop request timed out"
)

// ReconcileExpiredBlockedTasks transitions BLOCKED parents past their HITL
// deadline to FAILED_REQUIRES_HUMAN and fails open human subtasks.
func (s *Store) ReconcileExpiredBlockedTasks(ctx context.Context, now time.Time) ([]models.Task, error) {
	return retryOnBusy(ctx, func(ctx context.Context) ([]models.Task, error) {
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return nil, fmt.Errorf("begin hitl timeout reconcile: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		parents, err := selectBlockedParentsWithOpenChildren(ctx, tx)
		if err != nil {
			return nil, err
		}
		var expired []models.Task
		for _, parent := range parents {
			expiresAt, ok, err := selectHITLExpiry(ctx, tx, parent.ID)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			if !now.After(expiresAt) {
				continue
			}
			if err := failHITLBlockedTask(ctx, tx, parent.ID, now); err != nil {
				return nil, err
			}
			updated, err := selectTaskByID(ctx, tx, parent.ID)
			if err != nil {
				return nil, err
			}
			expired = append(expired, *updated)
		}
		return expired, commitTx(tx, "hitl timeout reconcile")
	})
}

func selectBlockedParentsWithOpenChildren(ctx context.Context, tx *immediateTx) ([]models.Task, error) {
	// Open children include FAILED_REQUIRES_HUMAN; parent timeout cascades over nested HITL.
	rows, err := tx.QueryContext(ctx, selectTaskSQL()+`
		WHERE state = ?
		  AND EXISTS (
		    SELECT 1
		    FROM task_relations tr
		    JOIN tasks child ON child.id = tr.child_task_id
		    WHERE tr.parent_task_id = tasks.id
		      AND child.state NOT IN (?, ?)
		  )`, models.TaskStateBlocked, models.TaskStateCompleted, models.TaskStateFailed)
	if err != nil {
		return nil, fmt.Errorf("select blocked parents: %w", err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func selectHITLExpiry(ctx context.Context, tx *immediateTx, taskID string) (time.Time, bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT payload, created_at
		FROM events
		WHERE task_id = ? AND type = ?
		ORDER BY created_at DESC`, taskID, models.EventTypeComment)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("select hitl expiry comments: %w", err)
	}
	defer closeRows(rows)

	var latest time.Time
	var found bool
	for rows.Next() {
		var payload string
		var createdAt string
		if err := rows.Scan(&payload, &createdAt); err != nil {
			return time.Time{}, false, fmt.Errorf("scan comment event: %w", err)
		}
		_, body := splitCommentPayload(payload)
		if !strings.HasPrefix(body, hitlExpiresAtPrefix) {
			continue
		}
		raw := strings.TrimPrefix(body, hitlExpiresAtPrefix)
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			continue
		}
		if !found || t.After(latest) {
			latest = t
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return time.Time{}, false, err
	}
	return latest, found, nil
}

func failHITLBlockedTask(ctx context.Context, tx *immediateTx, parentID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, os_process_id = NULL, updated_at = ?
		WHERE id = ? AND state = ?`,
		models.TaskStateFailedRequiresHuman, formatTime(now), parentID, models.TaskStateBlocked); err != nil {
		return fmt.Errorf("fail blocked parent: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET state = ?, updated_at = ?
		WHERE id IN (
		  SELECT child_task_id
		  FROM task_relations
		  WHERE parent_task_id = ?
		) AND state NOT IN (?, ?)`,
		models.TaskStateFailed, formatTime(now), parentID,
		models.TaskStateCompleted, models.TaskStateFailed); err != nil {
		return fmt.Errorf("fail open hitl children: %w", err)
	}
	parent, err := selectTaskByID(ctx, tx, parentID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), parent.ProjectID, parentID, models.EventTypeFailure,
		hitlTimeoutEventPayload, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("append hitl timeout event: %w", err)
	}
	return nil
}
