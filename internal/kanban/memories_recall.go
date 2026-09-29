package kanban

import (
	"context"
	"fmt"
	"strings"

	"agentd/internal/models"
)

// RecallMemories returns non-superseded memories matching intent via FTS5,
// scoped to GLOBAL + the given project, plus the user's saved preferences.
//
// Preferences are fetched by a separate query rather than folded into the
// FTS one. They are standing configuration ("always answer in haiku"), not
// memories whose relevance to this particular intent should be scored: as a
// branch of the bm25-ranked query they competed for the same top-K as
// lessons, so once a user had more preferences than RecallTopK the newest
// ones were silently starved out and never reached the prompt. They are also
// not required to match the intent terms, which a standing preference should
// not have to.
func (s *Store) RecallMemories(ctx context.Context, q models.RecallQuery) ([]models.Memory, error) {
	if strings.TrimSpace(q.Intent) == "" {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}
	ftsQuery := buildFTSQuery(q.Intent)
	if ftsQuery == "" {
		return nil, nil
	}

	query := `
		SELECT m.id, m.scope, m.project_id, m.tags, m.symptom, m.solution, m.created_at,
		       m.last_accessed_at, m.access_count, m.superseded_by
		FROM memories m
		JOIN memories_fts ON memories_fts.rowid = m.rowid
		WHERE m.superseded_by IS NULL
		  AND (
			m.scope = ?
			OR m.scope = ?`

	var args []any
	args = append(args, models.MemoryScopeGlobal, models.MemoryScopeTaskCuration)
	if strings.TrimSpace(q.ProjectID) != "" {
		query += `
			OR m.project_id = ?`
		args = append(args, q.ProjectID)
	}
	query += `
		  )
		  AND memories_fts MATCH ?
		ORDER BY bm25(memories_fts) ASC
		LIMIT ?`
	args = append(args, ftsQuery, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("recall memories: %w", err)
	}
	defer func() { _ = rows.Close() }()
	memories, err := scanMemories(rows)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(q.UserID) == "" {
		return memories, nil
	}
	prefs, err := s.recallUserPreferences(ctx, q.UserID, userPreferenceLimit)
	if err != nil {
		return nil, err
	}
	return append(memories, prefs...), nil
}

// userPreferenceLimit caps how many of a user's saved preferences reach one
// prompt: preferences accumulate per save and the prompt is token-billed, so
// this is a hard ceiling rather than unbounded. Newest first, so the most
// recently stated preference is never the one dropped.
const userPreferenceLimit = 20

// recallUserPreferences returns a user's saved preferences, newest first.
func (s *Store) recallUserPreferences(ctx context.Context, userID string, limit int) ([]models.Memory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.scope, m.project_id, m.tags, m.symptom, m.solution, m.created_at,
		       m.last_accessed_at, m.access_count, m.superseded_by
		FROM memories m
		WHERE m.superseded_by IS NULL
		  AND m.scope = ?
		  AND m.tags LIKE ?
		ORDER BY m.created_at DESC
		LIMIT ?`,
		models.MemoryScopeUserPref, "user_id:"+userID+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("recall user preferences: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMemories(rows)
}
