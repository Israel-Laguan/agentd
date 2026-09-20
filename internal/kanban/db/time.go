package db

import (
	"fmt"
	"time"
)

// UTCNow returns the current time in UTC at full clock precision.
//
// Timestamps double as optimistic-concurrency versions (updated_at): second
// truncation let a dispatcher claim/start an origin in the same UTC second
// as the resolver's read without changing the version, so the stale
// CompleteTieredOrigin write still matched. Keep sub-second precision so
// every committed write moves the version (stored as RFC3339Nano).
func UTCNow() time.Time { return time.Now().UTC() }

// FormatTime formats t as RFC3339Nano in UTC.
func FormatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

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
// NULL, non-nil becomes the RFC3339Nano string.
func NullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return FormatTime(*t)
}
