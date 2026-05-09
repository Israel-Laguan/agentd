package models

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// EncodeMemoryTags stores tags as a JSON array for round-trip safety.
func EncodeMemoryTags(tags []string) (sql.NullString, error) {
	if len(tags) == 0 {
		return sql.NullString{}, nil
	}
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		normalized = append(normalized, tag)
	}
	if len(normalized) == 0 {
		return sql.NullString{}, nil
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("marshal memory tags: %w", err)
	}
	return sql.NullString{String: string(raw), Valid: true}, nil
}

// DecodeMemoryTags parses JSON-array tags from storage.
func DecodeMemoryTags(tags sql.NullString) ([]string, error) {
	if !tags.Valid || strings.TrimSpace(tags.String) == "" {
		return nil, nil
	}
	var parsed []string
	if err := json.Unmarshal([]byte(tags.String), &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal memory tags: %w", err)
	}
	return parsed, nil
}
