package db

import "database/sql"

// CloseRows closes rows, discarding any error (safe for defer).
func CloseRows(rows *sql.Rows) { _ = rows.Close() }
