package models

import (
	"context"
	"strings"
)

// SettingKeyHouseRules is the settings row key for global standards injected
// into LLM system prompts (see gateway house-rules context).
const SettingKeyHouseRules = "house_rules"

// LoadHouseRules returns trimmed house rules text from the store, or empty
// string when unset or on read error (callers treat empty as disabled).
func LoadHouseRules(ctx context.Context, store KanbanStore) string {
	if store == nil {
		return ""
	}
	v, ok, err := store.GetSetting(ctx, SettingKeyHouseRules)
	if err != nil || !ok {
		return ""
	}
	return strings.TrimSpace(v)
}
