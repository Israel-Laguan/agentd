package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UTCNow returns the current time in UTC at full clock precision.
//
// Timestamps double as optimistic-concurrency versions (updated_at): second
// truncation let a dispatcher claim/start an origin in the same UTC second
// as the resolver's read without changing the version, so the stale
// CompleteTieredOrigin write still matched. Keep sub-second precision so
// every committed write moves the version (stored as fixed-width
// RFC3339Nano, see FormatTime).
func UTCNow() time.Time { return time.Now().UTC() }

// timestampLayout is RFC3339Nano with a fixed-width 9-digit fractional part.
// time.RFC3339Nano omits trailing zeros, so an exact-second timestamp ends in
// "05Z" while a fractional timestamp in the same second contains "05.123...Z".
// Since '.' (0x2E) sorts before 'Z' (0x5A), SQLite's lexicographic comparison
// orders the newer fractional row BEFORE the older exact-second row, breaking
// range queries (ListCommentsSince, UpdatedAfter filters) and stale-task
// checks. Always emitting 9 fractional digits keeps string order identical to
// chronological order.
const timestampLayout = "2006-01-02T15:04:05.000000000Z07:00"

// FormatTime formats t as fixed-width RFC3339Nano in UTC.
func FormatTime(t time.Time) string { return t.UTC().Format(timestampLayout) }

// ParseTime parses a stored timestamp string. Accepts RFC3339Nano (canonical)
// or "2006-01-02 15:04:05" (legacy SQLite default).
func ParseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err == nil {
		return parsed.UTC(), nil
	}
	if parsed, err2 := time.Parse("2006-01-02 15:04:05", value); err2 == nil {
		return parsed.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("parse timestamp %q: %w", value, err)
}

// NullableTime converts a *time.Time to a SQL-compatible value: nil becomes
// NULL, non-nil becomes the fixed-width RFC3339Nano string.
func NullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return FormatTime(*t)
}

// curatedTimestampPrefix marks tombstoned event rows (see Store.MarkEventsCurated
// in package kanban). Such rows are excluded from range queries, so their
// suffix is left untouched by normalization.
const curatedTimestampPrefix = "CURATED:"

// NormalizeTimestampString rewrites a stored timestamp into the canonical
// fixed-width form. It reports false for values that need no change
// (already canonical, empty, NULL sentinel, or the CURATED: tombstone prefix
// used by MarkEventsCurated) and for values that do not parse as timestamps.
func NormalizeTimestampString(value string) (string, bool) {
	if value == "" || strings.HasPrefix(value, curatedTimestampPrefix) {
		return value, false
	}
	parsed, err := ParseTime(value)
	if err != nil {
		return value, false
	}
	normalized := FormatTime(parsed)
	return normalized, normalized != value
}

// timestampColumns lists the TEXT timestamp columns normalized on open, as
// (table, primary key, columns). events.updated_at rows carrying the CURATED:
// tombstone prefix are left untouched by NormalizeTimestampString.
var timestampColumns = []struct {
	table   string
	pk      string
	columns []string
}{
	{"projects", "id", []string{"created_at", "updated_at"}},
	{"tasks", "id", []string{"created_at", "updated_at", "started_at", "completed_at", "last_heartbeat"}},
	{"events", "id", []string{"created_at", "updated_at"}},
	{"settings", "key", []string{"updated_at"}},
	{"agent_profiles", "id", []string{"updated_at"}},
	{"memories", "id", []string{"created_at", "last_accessed_at"}},
}

// NormalizeStoredTimestamps rewrites legacy variable-width timestamps (exact
// seconds or trimmed fractions from time.RFC3339Nano) into the canonical
// fixed-width form so lexicographic SQLite comparisons match chronological
// order. Without this, a database written before FormatTime became
// fixed-width mixes "05Z" and "05.123Z" spellings and range queries misorder
// rows within the same second. Values that do not parse as timestamps are
// left alone.
func NormalizeStoredTimestamps(ctx context.Context, db *sql.DB) error {
	for _, spec := range timestampColumns {
		if err := normalizeTableTimestamps(ctx, db, spec.table, spec.pk, spec.columns); err != nil {
			return err
		}
	}
	return nil
}

// timestampRewrite is a single stored value to rewrite into canonical form.
type timestampRewrite struct {
	pk     string
	column string
	value  string
}

func normalizeTableTimestamps(ctx context.Context, db *sql.DB, table, pk string, columns []string) error {
	exists, err := tableExists(ctx, db, table)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	cols := append([]string{pk}, columns...)
	rows, err := db.QueryContext(ctx, `SELECT `+strings.Join(cols, ", ")+` FROM `+table)
	if err != nil {
		return fmt.Errorf("normalize %s: %w", table, err)
	}
	pending, err := collectTimestampRewrites(rows, table, columns)
	if err != nil {
		return err
	}
	for _, r := range pending {
		if _, err := db.ExecContext(ctx, `UPDATE `+table+` SET `+r.column+` = ? WHERE `+pk+` = ?`, r.value, r.pk); err != nil {
			return fmt.Errorf("normalize %s.%s: %w", table, r.column, err)
		}
	}
	return nil
}

func collectTimestampRewrites(rows *sql.Rows, table string, columns []string) ([]timestampRewrite, error) {
	defer func() { _ = rows.Close() }()
	var pending []timestampRewrite
	width := len(columns) + 1
	for rows.Next() {
		vals := make([]sql.NullString, width)
		ptrs := make([]any, width)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scan %s timestamps: %w", table, err)
		}
		if !vals[0].Valid {
			continue
		}
		for i, col := range columns {
			v := vals[i+1]
			if !v.Valid {
				continue
			}
			if normalized, changed := NormalizeTimestampString(v.String); changed {
				pending = append(pending, timestampRewrite{pk: vals[0].String, column: col, value: normalized})
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s timestamps: %w", table, err)
	}
	return pending, nil
}
