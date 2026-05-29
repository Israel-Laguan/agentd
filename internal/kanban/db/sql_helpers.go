package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// RequireRowsAffected returns errOnMismatch when the number of rows affected
// by result does not equal want.
func RequireRowsAffected(result sql.Result, want int64, errOnMismatch error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if affected != want {
		return errOnMismatch
	}
	return nil
}

// Placeholders returns a comma-separated list of count "?" placeholders for
// use in IN (...) clauses.
func Placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

// TaskIDsAsAny converts a string slice to []any for use as variadic SQL args.
func TaskIDsAsAny(ids []string) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

// NullString converts a sql.NullString to a driver-compatible value (nil when
// not valid, plain string otherwise).
func NullString(ns sql.NullString) any {
	if !ns.Valid {
		return nil
	}
	return ns.String
}
