package kanban

import (
	"context"
	"testing"
)

func TestSettingsSetEmptyKey(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, "  ", "v"); err == nil {
		t.Fatal("expected error for empty key")
	}
}

func TestSettingsGetMissing(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	_, ok, err := store.GetSetting(ctx, "no-such-key")
	if err != nil || ok {
		t.Fatalf("GetSetting = ok=%v err=%v", ok, err)
	}
}

func TestSettingsUpsertRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, "alpha", "1"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	v, ok, err := store.GetSetting(ctx, "alpha")
	if err != nil || !ok || v != "1" {
		t.Fatalf("GetSetting alpha: ok=%v v=%q err=%v", ok, v, err)
	}
	if err := store.SetSetting(ctx, "alpha", "2"); err != nil {
		t.Fatalf("SetSetting update: %v", err)
	}
	v, ok, err = store.GetSetting(ctx, "alpha")
	if err != nil || !ok || v != "2" {
		t.Fatalf("GetSetting alpha after update: ok=%v v=%q err=%v", ok, v, err)
	}
}

func TestListSettingsRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	if err := store.SetSetting(ctx, "alpha", "one"); err != nil {
		t.Fatalf("SetSetting alpha: %v", err)
	}
	if err := store.SetSetting(ctx, "beta", "two"); err != nil {
		t.Fatalf("SetSetting beta: %v", err)
	}
	settings, err := store.ListSettings(ctx)
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	keys := map[string]string{}
	for _, s := range settings {
		if s.Key == "schema_version" {
			continue
		}
		keys[s.Key] = s.Value
		if s.UpdatedAt.IsZero() {
			t.Fatalf("setting %s has zero UpdatedAt", s.Key)
		}
	}
	if len(keys) != 2 || keys["alpha"] != "one" || keys["beta"] != "two" {
		t.Fatalf("settings = %#v", keys)
	}
}
