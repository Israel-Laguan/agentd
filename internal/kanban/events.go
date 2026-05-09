package kanban

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"agentd/internal/models"

	"github.com/google/uuid"
)

func (s *Store) AddComment(ctx context.Context, c models.Comment) error {
	if c.ID == "" {
		c.ID = uuid.NewString()
	}

	return retryOnBusyNoResult(ctx, func(ctx context.Context) error {
		now := utcNow()
		tx, err := beginImmediate(ctx, s.db)
		if err != nil {
			return fmt.Errorf("begin add comment: %w", err)
		}
		defer rollbackUnlessCommitted(tx)

		task, err := selectTaskByID(ctx, tx, c.TaskID)
		if err != nil {
			return fmt.Errorf("resolve task for comment: %w", err)
		}
		if strings.TrimSpace(c.Body) == "" {
			c.Body = strings.TrimSpace(c.Content)
		}
		author := models.NormalizeCommentAuthor(string(c.Author))
		payload := fmt.Sprintf("%s: %s", author, c.Body)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			c.ID, task.ProjectID, c.TaskID, models.EventTypeComment, payload, formatTime(now), formatTime(now)); err != nil {
			return fmt.Errorf("add comment event: %w", err)
		}
		if author != models.CommentAuthorUser {
			return commitTx(tx, "add comment")
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE tasks
			SET state = ?, assignee = ?, updated_at = ?
			WHERE id = ?`,
			models.TaskStateInConsideration, models.TaskAssigneeHuman, formatTime(now), c.TaskID); err != nil {
			return fmt.Errorf("move task into consideration: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit add comment: %w", err)
		}
		if s.canceller != nil {
			s.canceller.Cancel(c.TaskID)
		}
		return nil
	})
}

// AddCommentAndPause satisfies the proposal-aligned board contract by forcing a
// human-authored pause transition through the existing AddComment workflow.
func (s *Store) AddCommentAndPause(ctx context.Context, taskID string, comment models.Comment) error {
	comment.TaskID = taskID
	if strings.TrimSpace(string(comment.Author)) == "" {
		comment.Author = models.CommentAuthorUser
	}
	if strings.TrimSpace(comment.Body) == "" && strings.TrimSpace(comment.Content) != "" {
		comment.Body = comment.Content
	}
	if strings.TrimSpace(comment.Content) == "" && strings.TrimSpace(comment.Body) != "" {
		comment.Content = comment.Body
	}
	return s.AddComment(ctx, comment)
}

func (s *Store) ListComments(ctx context.Context, taskID string) ([]models.Comment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, payload, created_at, updated_at
		FROM events
		WHERE task_id = ? AND type = ?
		ORDER BY created_at`, taskID, models.EventTypeComment)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	defer closeRows(rows)
	return scanComments(rows)
}

func scanComments(rows *sql.Rows) ([]models.Comment, error) {
	var comments []models.Comment
	for rows.Next() {
		var comment models.Comment
		var payload, createdAt, updatedAt string
		if err := rows.Scan(&comment.ID, &comment.TaskID, &payload, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		author, body := splitCommentPayload(payload)
		comment.Author, comment.Body = models.NormalizeCommentAuthor(author), body
		comment.Content = comment.Body
		created, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		updated, err := parseTime(updatedAt)
		if err != nil {
			return nil, err
		}
		comment.CreatedAt = created
		comment.UpdatedAt = updated
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate comments: %w", err)
	}
	return comments, nil
}

func splitCommentPayload(payload string) (string, string) {
	author, body, ok := strings.Cut(payload, ":")
	if !ok {
		return "", strings.TrimSpace(payload)
	}
	return strings.TrimSpace(author), strings.TrimSpace(body)
}

func (s *Store) ListEventsByTask(ctx context.Context, taskID string) ([]models.Event, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, task_id, type, payload, created_at, updated_at
		FROM events
		WHERE task_id = ?
		ORDER BY created_at`, taskID)
	if err != nil {
		return nil, fmt.Errorf("list events by task: %w", err)
	}
	defer closeRows(rows)
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]models.Event, error) {
	var out []models.Event
	for rows.Next() {
		var e models.Event
		var taskID sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&e.ID, &e.ProjectID, &taskID, &e.Type, &e.Payload, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.TaskID = taskID
		created, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		e.CreatedAt = created
		if updatedAt != "" {
			updated, err := parseTime(updatedAt)
			if err != nil {
				return nil, err
			}
			e.UpdatedAt = updated
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	return out, nil
}

const curatedPrefix = "CURATED:"

func (s *Store) MarkEventsCurated(ctx context.Context, taskID string) error {
	now := utcNow()
	sentinel := curatedPrefix + formatTime(now)
	_, err := s.db.ExecContext(ctx, `
		UPDATE events SET updated_at = ?
		WHERE task_id = ? AND updated_at NOT LIKE 'CURATED:%'`,
		sentinel, taskID)
	if err != nil {
		return fmt.Errorf("mark events curated: %w", err)
	}
	return nil
}

func (s *Store) DeleteCuratedEvents(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM events
		WHERE task_id = ? AND updated_at LIKE 'CURATED:%'`,
		taskID)
	if err != nil {
		return fmt.Errorf("delete curated events: %w", err)
	}
	return nil
}

func (s *Store) ListCompletedTasksOlderThan(ctx context.Context, age time.Duration) ([]models.Task, error) {
	cutoff := utcNow().Add(-age)
	rows, err := s.db.QueryContext(ctx, selectTaskSQL()+`
		WHERE state = ? AND updated_at < ?
		AND id NOT IN (
			SELECT DISTINCT task_id FROM events
			WHERE task_id IS NOT NULL AND updated_at LIKE 'CURATED:%'
		)
		ORDER BY updated_at`,
		models.TaskStateCompleted, formatTime(cutoff))
	if err != nil {
		return nil, fmt.Errorf("list completed tasks older than %v: %w", age, err)
	}
	defer closeRows(rows)
	return scanTasks(rows)
}

func (s *Store) AppendEvent(ctx context.Context, e models.Event) error {
	event, err := normalizeEvent(e)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events (id, project_id, task_id, type, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.ProjectID, nullString(event.TaskID), event.Type, event.Payload,
		formatTime(event.CreatedAt), formatTime(event.UpdatedAt))
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func normalizeEvent(e models.Event) (models.Event, error) {
	now := utcNow()
	if e.ID == "" {
		e.ID = uuid.NewString()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if strings.TrimSpace(e.ProjectID) == "" {
		return models.Event{}, fmt.Errorf("%w: event project id is required", models.ErrInvalidDraftPlan)
	}
	if strings.TrimSpace(string(e.Type)) == "" {
		return models.Event{}, fmt.Errorf("%w: event type is required", models.ErrInvalidDraftPlan)
	}
	return e, nil
}
