package db

import (
	"database/sql"
	"fmt"

	"agentd/internal/models"
)

// ScanSettings scans all rows from a settings query result.
func ScanSettings(rows *sql.Rows) ([]models.Setting, error) {
	var out []models.Setting
	for rows.Next() {
		setting, err := ScanSetting(rows)
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

// ScanSetting scans one row into a models.Setting.
func ScanSetting(row Scanner) (models.Setting, error) {
	var st models.Setting
	var updatedAt string
	if err := row.Scan(&st.Key, &st.Value, &updatedAt); err != nil {
		return models.Setting{}, fmt.Errorf("scan setting: %w", err)
	}
	parsed, err := ParseTime(updatedAt)
	if err != nil {
		return models.Setting{}, err
	}
	st.UpdatedAt = parsed
	return st, nil
}
