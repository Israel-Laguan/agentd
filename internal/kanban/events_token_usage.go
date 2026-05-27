package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"agentd/internal/models"
)

// tokenUsagePayload is the JSON shape stored in TOKEN_USAGE event payloads.
type tokenUsagePayload struct {
	Tokens int `json:"tokens"`
}

// ListTokenUsageEventsSince returns per-call token usage events at or after since.
func (s *Store) ListTokenUsageEventsSince(ctx context.Context, since time.Time) ([]models.TokenUsageEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT created_at, payload
		FROM events
		WHERE type = ? AND created_at >= ?
		ORDER BY created_at`,
		models.EventTypeTokenUsage, formatTime(since.UTC()))
	if err != nil {
		return nil, fmt.Errorf("list token usage events since: %w", err)
	}
	defer closeRows(rows)

	var out []models.TokenUsageEvent
	for rows.Next() {
		var createdAt, payload string
		if err := rows.Scan(&createdAt, &payload); err != nil {
			return nil, fmt.Errorf("scan token usage event: %w", err)
		}
		at, err := parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		tokens, ok := parseTokenUsagePayload(payload)
		if !ok || tokens <= 0 {
			continue
		}
		out = append(out, models.TokenUsageEvent{At: at, Tokens: tokens})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token usage events: %w", err)
	}
	return out, nil
}

func parseTokenUsagePayload(payload string) (int, bool) {
	var p tokenUsagePayload
	if err := json.Unmarshal([]byte(payload), &p); err == nil && p.Tokens > 0 {
		return p.Tokens, true
	}
	var n int
	if _, err := fmt.Sscanf(payload, "%d", &n); err == nil && n > 0 {
		return n, true
	}
	return 0, false
}
