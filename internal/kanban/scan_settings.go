package kanban

import (
	"database/sql"
	"fmt"

	"agentd/internal/models"
)

func scanSettings(rows *sql.Rows) ([]models.Setting, error) {
	var out []models.Setting
	for rows.Next() {
		setting, err := scanSetting(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, setting)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settings: %w", err)
	}
	return out, nil
}

func scanSetting(row scanner) (models.Setting, error) {
	var st models.Setting
	var updatedAt string
	if err := row.Scan(&st.Key, &st.Value, &updatedAt); err != nil {
		return models.Setting{}, fmt.Errorf("scan setting: %w", err)
	}
	parsed, err := parseTime(updatedAt)
	if err != nil {
		return models.Setting{}, err
	}
	st.UpdatedAt = parsed
	return st, nil
}
