package kanban

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"agentd/internal/models"

	"github.com/google/uuid"
)

func (s *Store) RecordMemory(ctx context.Context, m models.Memory) error {
	now := utcNow()
	if strings.TrimSpace(m.ID) == "" {
		m.ID = uuid.NewString()
	}
	if strings.TrimSpace(string(m.Scope)) == "" {
		m.Scope = models.MemoryScopeGlobal
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO memories (id, scope, project_id, tags, symptom, solution, created_at, last_accessed_at, access_count, superseded_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.Scope, nullString(m.ProjectID), nullString(m.Tags),
		nullString(m.Symptom), nullString(m.Solution), formatTime(m.CreatedAt),
		nullString(m.LastAccessedAt), m.AccessCount, nullString(m.SupersededBy))
	if err != nil {
		return fmt.Errorf("record memory: %w", err)
	}
	return nil
}

func (s *Store) ListMemories(ctx context.Context, filter models.MemoryFilter) ([]models.Memory, error) {
	query := `
		SELECT id, scope, project_id, tags, symptom, solution, created_at,
		       last_accessed_at, access_count, superseded_by
		FROM memories`
	var args []any
	var clauses []string
	if strings.TrimSpace(string(filter.Scope)) != "" {
		clauses = append(clauses, "scope = ?")
		args = append(args, filter.Scope)
	}
	if filter.ProjectID.Valid {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ProjectID.String)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	for _, tag := range filter.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if len(clauses) == 0 {
			query += " WHERE "
		} else {
			query += " AND "
		}
		clauses = append(clauses, "tags LIKE ?")
		args = append(args, "%\""+tag+"\"%")
		query += "tags LIKE ?"
	}
	query += " ORDER BY created_at"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer closeRows(rows)
	return scanMemories(rows)
}

// RecallMemories returns non-superseded memories matching intent via FTS5,
// scoped to GLOBAL + the given project + user preferences.
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
	if strings.TrimSpace(q.UserID) != "" {
		query += `
			OR (m.scope = ? AND m.tags LIKE ?)`
		args = append(args, models.MemoryScopeUserPref)
		args = append(args, "user_id:"+q.UserID+"%")
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
	defer closeRows(rows)
	return scanMemories(rows)
}

// TouchMemories bumps access_count and last_accessed_at for the given IDs.
func (s *Store) TouchMemories(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(ids)+1)
	args = append(args, formatTime(utcNow()))
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE memories
		SET access_count = access_count + 1,
		    last_accessed_at = ?
		WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return fmt.Errorf("touch memories: %w", err)
	}
	return nil
}

// SupersedeMemories marks a set of memory IDs as superseded by newID.
func (s *Store) SupersedeMemories(ctx context.Context, oldIDs []string, newID string) error {
	if len(oldIDs) == 0 {
		return nil
	}
	placeholders := strings.Repeat("?,", len(oldIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(oldIDs)+1)
	args = append(args, newID)
	for _, id := range oldIDs {
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE memories SET superseded_by = ?
		WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return fmt.Errorf("supersede memories: %w", err)
	}
	return nil
}

// ListUnsupersededMemories returns all memories that have not been superseded.
func (s *Store) ListUnsupersededMemories(ctx context.Context) ([]models.Memory, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, scope, project_id, tags, symptom, solution, created_at,
		       last_accessed_at, access_count, superseded_by
		FROM memories
		WHERE superseded_by IS NULL
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list unsuperseded memories: %w", err)
	}
	defer closeRows(rows)
	return scanMemories(rows)
}

// buildFTSQuery converts free-text intent into an OR-joined FTS5 query.
func buildFTSQuery(intent string) string {
	words := strings.Fields(intent)
	var terms []string
	for _, w := range words {
		clean := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				return r
			}
			return -1
		}, w)
		if len(clean) >= 2 {
			terms = append(terms, clean)
		}
	}
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " OR ")
}

func scanMemory(row scanner) (*models.Memory, error) {
	var memory models.Memory
	var createdAt string
	if err := row.Scan(
		&memory.ID,
		&memory.Scope,
		&memory.ProjectID,
		&memory.Tags,
		&memory.Symptom,
		&memory.Solution,
		&createdAt,
		&memory.LastAccessedAt,
		&memory.AccessCount,
		&memory.SupersededBy,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, err
		}
		return nil, fmt.Errorf("scan memory: %w", err)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	memory.CreatedAt = created
	return &memory, nil
}

func scanMemories(rows *sql.Rows) ([]models.Memory, error) {
	var out []models.Memory
	for rows.Next() {
		memory, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *memory)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memories: %w", err)
	}
	return out, nil
}
