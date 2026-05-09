package kanban

import "database/sql"

func closeRows(rows *sql.Rows) {
	_ = rows.Close()
}
