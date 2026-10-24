package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// TestMigrationAddsAllowedToolsAndWorkspaceReadonly upgrades a v24 database,
// whose agent_profiles rows predate the tool allowlist, and checks both new
// columns exist with defaults that keep existing profiles unrestricted.
func TestMigrationAddsAllowedToolsAndWorkspaceReadonly(t *testing.T) {
	db, err := sql.Open("sqlite", "file:migrate-v25-agent-tools?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, v24AgentProfilesSQL); err != nil {
		t.Fatalf("create v24 agent_profiles: %v", err)
	}

	if err := Run(ctx, db, t.TempDir()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'schema_version'`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != "25" {
		t.Fatalf("schema version = %q, want 25", version)
	}

	for _, col := range []string{"allowed_tools", "workspace_readonly"} {
		var hasColumn int
		if err := db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM pragma_table_info('agent_profiles') WHERE name = ?`, col).Scan(&hasColumn); err != nil {
			t.Fatalf("check agent_profiles.%s column: %v", col, err)
		}
		if hasColumn != 1 {
			t.Fatalf("agent_profiles.%s column present = %d, want 1", col, hasColumn)
		}
	}

	var allowed string
	var readonly int
	if err := db.QueryRowContext(ctx, `
		SELECT allowed_tools, workspace_readonly FROM agent_profiles WHERE id = 'default'`).Scan(&allowed, &readonly); err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if allowed != "[]" || readonly != 0 {
		t.Fatalf("allowed_tools = %q, workspace_readonly = %d; want [] and 0 for existing profiles", allowed, readonly)
	}
}

const v24AgentProfilesSQL = `
CREATE TABLE settings (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE agent_profiles (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    temperature REAL NOT NULL DEFAULT 0.7,
    system_prompt TEXT,
    role TEXT NOT NULL DEFAULT 'CODE_GEN',
    max_tokens INTEGER NOT NULL DEFAULT 0,
    agentic_mode INTEGER NOT NULL DEFAULT 0,
    disable_topic_drift INTEGER NOT NULL DEFAULT 0,
    capability_route_intent TEXT,
    updated_at TEXT NOT NULL
) STRICT;

INSERT INTO settings (key, value, updated_at) VALUES ('schema_version', '24', datetime('now'));
INSERT INTO agent_profiles (id, name, provider, model, updated_at)
VALUES ('default', 'Default', 'openai', 'gpt-4o-mini', datetime('now'));
`
