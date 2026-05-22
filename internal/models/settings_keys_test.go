package models_test

import (
	"context"
	"testing"

	"agentd/internal/models"
	"agentd/internal/testutil"
)

func TestLoadHouseRules(t *testing.T) {
	ctx := context.Background()

	t.Run("nil store", func(t *testing.T) {
		if got := models.LoadHouseRules(ctx, nil); got != "" {
			t.Fatalf("LoadHouseRules(nil store) = %q, want empty", got)
		}
	})

	t.Run("missing key", func(t *testing.T) {
		store := testutil.NewFakeStore()
		if got := models.LoadHouseRules(ctx, store); got != "" {
			t.Fatalf("LoadHouseRules(missing key) = %q, want empty", got)
		}
	})

	t.Run("trimmed value", func(t *testing.T) {
		store := testutil.NewFakeStore()
		if err := store.SetSetting(ctx, models.SettingKeyHouseRules, " rules "); err != nil {
			t.Fatalf("SetSetting: %v", err)
		}
		if got := models.LoadHouseRules(ctx, store); got != "rules" {
			t.Fatalf("LoadHouseRules(trimmed) = %q, want %q", got, "rules")
		}
	})
}
