package migrations

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
)

// TestMigrateToV28_AddsGatedTools: a v24 profile row gains gated_tools as "[]",
// so it gates nothing; running again changes nothing.
func TestMigrateToV28_AddsGatedTools(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v28-gated-tools?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v24AgentProfilesSQL); err != nil {
		t.Fatalf("create v24 agent_profiles: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := Run(ctx, db, t.TempDir()); err != nil {
			t.Fatalf("Run #%d: %v", i+1, err)
		}
	}
	var gated string
	if err := db.QueryRowContext(ctx, `SELECT gated_tools FROM agent_profiles WHERE id = 'default'`).Scan(&gated); err != nil {
		t.Fatalf("read gated_tools: %v", err)
	}
	if gated != "[]" {
		t.Fatalf("gated_tools = %q, want [] for an existing profile", gated)
	}
	assertSchemaVersion(t, db, ctx, strconv.Itoa(currentSchemaVersion))
}
