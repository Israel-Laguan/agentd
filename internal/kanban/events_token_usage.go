package kanban

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agentd/internal/models"
)

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
		p, ok := parseTokenUsagePayload(payload)
		if !ok || p.Tokens <= 0 {
			continue
		}
		out = append(out, models.TokenUsageEvent{
			At:               at,
			Tokens:           p.Tokens,
			CachedTokens:     p.CachedTokens,
			CacheWriteTokens: p.CacheWriteTokens,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate token usage events: %w", err)
	}
	return out, nil
}

// parseTokenUsagePayload decodes a TOKEN_USAGE event payload into the shared
// models.TokenUsagePayload shape. It tolerates both the JSON object form
// ({"tokens":N,...}) and the legacy bare-integer form ("N"). Malformed
// payloads return ok=false so callers can skip them.
func parseTokenUsagePayload(payload string) (models.TokenUsagePayload, bool) {
	var p models.TokenUsagePayload
	if err := json.Unmarshal([]byte(payload), &p); err == nil && p.Tokens > 0 {
		return p, true
	}
	if n, err := strconv.Atoi(strings.TrimSpace(payload)); err == nil && n > 0 {
		return models.TokenUsagePayload{Tokens: n}, true
	}
	return models.TokenUsagePayload{}, false
}
