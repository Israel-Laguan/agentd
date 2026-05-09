package kanban

import (
	"database/sql"
	"fmt"
	"strings"
)

func requireRowsAffected(result sql.Result, want int64, errOnMismatch error) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read rows affected: %w", err)
	}
	if affected != want {
		return errOnMismatch
	}
	return nil
}

func placeholders(count int) string {
	if count <= 0 {
		return ""
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func taskIDsAsAny(ids []string) []any {
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}
