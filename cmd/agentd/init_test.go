package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"agentd/internal/kanban"
)

func TestInitVerbosePrintsConfig(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "--verbose", "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agentd init error = %v", err)
	}
	out := output.String()
	if !strings.Contains(out, "gateway.order cascade") {
		t.Errorf("verbose init output missing profile cascade hint\n%s", out)
	}
	for _, expect := range []string{"home=", "db_path=", "api.address=", "cron.path="} {
		if !strings.Contains(out, expect) {
			t.Errorf("verbose output missing %q\n%s", expect, out)
		}
	}
}

func TestInit_NonWritableHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(home, 0o555); err != nil {
		t.Skipf("cannot chmod (may be running as root): %v", err)
	}
	if f, err := os.CreateTemp(home, ".write-probe-*"); err == nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		t.Skip("home still writable in this environment (likely privileged user)")
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })

	err := execInitCLI(t, home)
	if err == nil {
		t.Fatal("agentd init on read-only home: error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "writable") && !strings.Contains(err.Error(), "permission") {
		t.Fatalf("init error = %v, want writable/permission failure", err)
	}
}

func TestInitCreatesHomeDatabaseAndWAL(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agentd init error = %v", err)
	}
	out := output.String()
	if !strings.Contains(out, "gateway.order cascade") {
		t.Errorf("init output missing profile cascade hint\n%s", out)
	}
	if !strings.Contains(out, "PATCH /api/v1/agents/<id>") {
		t.Errorf("init output missing profile PATCH hint\n%s", out)
	}

	assertPathExists(t, home)
	dbPath := filepath.Join(home, "global.db")
	assertPathExists(t, dbPath)
	assertPathExists(t, filepath.Join(home, "agentd.crontab"))
	assertJournalMode(t, dbPath, "wal")
}

func TestInitDoesNotOverwriteUserCron(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	cmd := newRootCommand()
	cmd.SetArgs([]string{"--home", home, "init"})
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("first agentd init error = %v", err)
	}

	cronPath := filepath.Join(home, "agentd.crontab")
	userCron := "@every 11s intake\n"
	if err := os.WriteFile(cronPath, []byte(userCron), 0o644); err != nil {
		t.Fatalf("write user cron: %v", err)
	}

	cmd = newRootCommand()
	cmd.SetArgs([]string{"--home", home, "init"})
	output.Reset()
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("second agentd init error = %v", err)
	}

	contents, err := os.ReadFile(cronPath)
	if err != nil {
		t.Fatalf("read cron file: %v", err)
	}
	if string(contents) != userCron {
		t.Fatalf("cron file = %q, want %q", contents, userCron)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected path %s to exist: %v", path, err)
	}
}

func assertJournalMode(t *testing.T, dbPath, want string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	defer func() { _ = db.Close() }()

	var got string
	if err := db.QueryRow("PRAGMA journal_mode;").Scan(&got); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if !strings.EqualFold(got, want) {
		t.Fatalf("journal_mode = %s, want %s", got, want)
	}
}

// TestSeedProfilesHaveEmptyProvider verifies that freshly seeded profiles have
// empty provider and model strings so they cascade through gateway.order.
func TestSeedProfilesHaveEmptyProvider(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	store, err := openTestStore(t, filepath.Join(home, "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	if err := seedDefaultAgent(context.Background(), store, false); err != nil {
		t.Fatalf("seedDefaultAgent: %v", err)
	}

	for _, id := range []string{"default", "researcher", "qa"} {
		p, err := store.GetAgentProfile(context.Background(), id)
		if err != nil {
			t.Fatalf("GetAgentProfile(%s): %v", id, err)
		}
		if p.Provider != "" {
			t.Errorf("profile %s: Provider = %q, want empty (gateway cascade)", id, p.Provider)
		}
		if p.Model != "" {
			t.Errorf("profile %s: Model = %q, want empty (gateway cascade)", id, p.Model)
		}
	}
}

// TestInitDoesNotOverwriteExistingProfile verifies that a repeated "agentd init"
// (without --reset-profiles) preserves operator PATCHes to the default profile.
func TestInitDoesNotOverwriteExistingProfile(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	store, err := openTestStore(t, filepath.Join(home, "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// First seed.
	if err := seedDefaultAgent(context.Background(), store, false); err != nil {
		t.Fatalf("first seedDefaultAgent: %v", err)
	}

	// Simulate an operator PATCH.
	p, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	p.Provider = "gemini"
	p.Model = "gemini-2.5-flash"
	if err := store.UpsertAgentProfile(context.Background(), *p); err != nil {
		t.Fatalf("UpsertAgentProfile (patch): %v", err)
	}

	// Second seed — must not overwrite.
	if err := seedDefaultAgent(context.Background(), store, false); err != nil {
		t.Fatalf("second seedDefaultAgent: %v", err)
	}

	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile after re-seed: %v", err)
	}
	if got.Provider != "gemini" {
		t.Errorf("Provider = %q after re-seed, want %q", got.Provider, "gemini")
	}
	if got.Model != "gemini-2.5-flash" {
		t.Errorf("Model = %q after re-seed, want %q", got.Model, "gemini-2.5-flash")
	}
}

// TestInitResetProfilesFlag verifies that --reset-profiles overwrites existing profiles.
func TestInitResetProfilesFlag(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	store, err := openTestStore(t, filepath.Join(home, "global.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Seed and patch.
	if err := seedDefaultAgent(context.Background(), store, false); err != nil {
		t.Fatalf("first seedDefaultAgent: %v", err)
	}
	p, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	p.Provider = "gemini"
	p.Model = "gemini-2.5-flash"
	if err := store.UpsertAgentProfile(context.Background(), *p); err != nil {
		t.Fatalf("UpsertAgentProfile (patch): %v", err)
	}

	// Re-seed with reset=true — must overwrite back to defaults.
	if err := seedDefaultAgent(context.Background(), store, true); err != nil {
		t.Fatalf("seedDefaultAgent(reset=true): %v", err)
	}

	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile after reset: %v", err)
	}
	if got.Provider != "" {
		t.Errorf("Provider = %q after reset, want empty", got.Provider)
	}
	if got.Model != "" {
		t.Errorf("Model = %q after reset, want empty", got.Model)
	}
}

// TestInitCLIResetProfilesFlag exercises the --reset-profiles flag through the init CLI.
func TestInitCLIResetProfilesFlag(t *testing.T) {
	home := filepath.Join(t.TempDir(), ".agentd")
	dbPath := filepath.Join(home, "global.db")

	if err := execInitCLI(t, home); err != nil {
		t.Fatalf("first agentd init: %v", err)
	}

	store, err := openTestStore(t, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	patchDefaultProfileGemini(t, store)
	_ = store.Close()

	if err := execInitCLI(t, home); err != nil {
		t.Fatalf("second agentd init (no reset): %v", err)
	}
	store, err = openTestStore(t, dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	assertDefaultProfileGemini(t, store)
	_ = store.Close()

	if err := execInitCLI(t, home, "--reset-profiles"); err != nil {
		t.Fatalf("agentd init --reset-profiles: %v", err)
	}
	store, err = openTestStore(t, dbPath)
	if err != nil {
		t.Fatalf("reopen store after reset: %v", err)
	}
	defer func() { _ = store.Close() }()
	assertDefaultProfileEmpty(t, store)
}

func execInitCLI(t *testing.T, home string, initArgs ...string) error {
	t.Helper()
	cmd := newRootCommand()
	cmd.SetArgs(append([]string{"--home", home, "init"}, initArgs...))
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	return cmd.ExecuteContext(context.Background())
}

func patchDefaultProfileGemini(t *testing.T, store *kanban.Store) {
	t.Helper()
	p, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	p.Provider = "gemini"
	p.Model = "gemini-2.5-flash"
	if err := store.UpsertAgentProfile(context.Background(), *p); err != nil {
		t.Fatalf("UpsertAgentProfile (patch): %v", err)
	}
}

func assertDefaultProfileGemini(t *testing.T, store *kanban.Store) {
	t.Helper()
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if got.Provider != "gemini" || got.Model != "gemini-2.5-flash" {
		t.Fatalf("profile = %+v, want patched gemini values preserved", got)
	}
}

func assertDefaultProfileEmpty(t *testing.T, store *kanban.Store) {
	t.Helper()
	got, err := store.GetAgentProfile(context.Background(), "default")
	if err != nil {
		t.Fatalf("GetAgentProfile: %v", err)
	}
	if got.Provider != "" {
		t.Errorf("Provider = %q after reset, want empty", got.Provider)
	}
	if got.Model != "" {
		t.Errorf("Model = %q after reset, want empty", got.Model)
	}
}

func openTestStore(t *testing.T, dbPath string) (*kanban.Store, error) {
	t.Helper()
	return kanban.OpenStore(dbPath)
}
