package db

import (
	"fmt"
	"time"
)

// UTCNow returns the current time in UTC with nanoseconds truncated to zero.
func UTCNow() time.Time { return time.Now().UTC().Round(0) }

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
