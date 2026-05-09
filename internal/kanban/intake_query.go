package kanban

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agentd/internal/models"

	"github.com/google/uuid"
)

func (s *Store) ListUnprocessedHumanComments(ctx context.Context) ([]models.CommentRef, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.id, e.id, e.payload, e.created_at
		FROM tasks t
		JOIN events e ON e.task_id = t.id AND e.type = ?
		WHERE t.state = ? AND t.assignee = ?
		  AND (e.payload LIKE 'USER:%' OR e.payload LIKE 'Human:%')
		  AND NOT EXISTS (
		    SELECT 1 FROM events done
		    WHERE done.task_id = t.id
		      AND done.type = ?
		      AND done.payload = e.id
		  )
		ORDER BY e.created_at`, models.EventTypeComment, models.TaskStateInConsideration, models.TaskAssigneeHuman, models.EventTypeCommentIntake)
	if err != nil {
		return nil, fmt.Errorf("list unprocessed human comments: %w", err)
	}
	defer closeRows(rows)
	return scanCommentRefs(rows)
}

func scanCommentRefs(rows *sql.Rows) ([]models.CommentRef, error) {
	var refs []models.CommentRef
	for rows.Next() {
		var ref models.CommentRef
		var createdAt string
		if err := rows.Scan(&ref.TaskID, &ref.CommentEventID, &ref.Body, &createdAt); err != nil {
			return nil, fmt.Errorf("scan comment intake ref: %w", err)
		}
		ref.Body = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(ref.Body, "USER:"), "Human:"))
		updated, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		ref.UpdatedAt = updated
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comment intake refs: %w", err)
	}
	return refs, nil
}

func (s *Store) MarkCommentProcessed(ctx context.Context, taskID, commentEventID string) error {
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	now := utcNow()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), task.ProjectID, taskID, models.EventTypeCommentIntake, commentEventID, formatTime(now), formatTime(now))
	if err != nil {
		return fmt.Errorf("mark comment processed: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET assignee = ?, updated_at = ?
		WHERE id = ?`,
		models.TaskAssigneeSystem, formatTime(now), taskID); err != nil {
		return fmt.Errorf("release comment task to system: %w", err)
	}
	return nil
}
